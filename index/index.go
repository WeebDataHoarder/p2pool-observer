package index

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/floatdrop/lru"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"golang.org/x/exp/slices"
	"log"
	"reflect"
	"regexp"
	"runtime/pprof"
	"strings"
	"sync"
)

type Index struct {
	consensus *sidechain.Consensus

	getDifficultyByHeight block.GetDifficultyByHeightFunc
	getSeedByHeight       block.GetSeedByHeightFunc
	getByTemplateId       sidechain.GetByTemplateIdFunc
	derivationCache       sidechain.DerivationCacheInterface
	blockCache            *lru.LRU[types.Hash, *sidechain.PoolBlock]

	handle     *sql.DB
	statements struct {
		GetMinerById            *sql.Stmt
		GetMinerByAddress       *sql.Stmt
		GetMinerByAlias         *sql.Stmt
		InsertMiner             *sql.Stmt
		TipSideBlock            *sql.Stmt
		TipSideBlocksTemplateId *sql.Stmt
		InsertOrUpdateSideBlock *sql.Stmt
		TipMainBlock            *sql.Stmt
		GetMainBlockByHeight    *sql.Stmt
		GetMainBlockById        *sql.Stmt
		GetSideBlockByMainId    *sql.Stmt
		GetSideBlockByUncleId   *sql.Stmt
	}
	caches struct {
		minerLock sync.RWMutex
		miner     map[uint64]*Miner
	}

	views map[string]string
}

//go:embed schema.sql
var dbSchema string

func OpenIndex(connStr string, consensus *sidechain.Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getSeedByHeight block.GetSeedByHeightFunc, getByTemplateId sidechain.GetByTemplateIdFunc) (index *Index, err error) {
	index = &Index{
		consensus:             consensus,
		getDifficultyByHeight: difficultyByHeight,
		getSeedByHeight:       getSeedByHeight,
		getByTemplateId:       getByTemplateId,
		derivationCache:       sidechain.NewDerivationLRUCache(),
		blockCache:            lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4)),
		views:                 make(map[string]string),
	}
	if index.handle, err = sql.Open("postgres", connStr); err != nil {
		return nil, err
	}

	index.handle.SetMaxIdleConns(8)

	tx, err := index.handle.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	viewMatch := regexp.MustCompile("CREATE (MATERIALIZED |)VIEW ([^ \n\t]+?)(_v[0-9]+)? AS\n")
	immv := regexp.MustCompile("SELECT (create_immv)\\('([^ \n\t']+?)(_v[0-9]+)?',")
	for _, statement := range strings.Split(dbSchema, ";") {
		var matches []string
		if matches = viewMatch.FindStringSubmatch(statement); matches == nil {
			matches = immv.FindStringSubmatch(statement)
		}
		if matches != nil {
			isMaterialized := matches[1] == "MATERIALIZED "
			isImmv := matches[1] == "create_immv"
			viewName := matches[2]
			fullViewName := viewName
			//base view for materialized one

			if len(matches[3]) != 0 {
				fullViewName = viewName + matches[3]
			}
			index.views[viewName] = fullViewName
			if row, err := tx.Query(fmt.Sprintf("SELECT relname, relkind FROM pg_class WHERE relname LIKE '%s%%';", viewName)); err != nil {
				return nil, err
			} else {
				var entries []struct{ n, kind string }
				var exists bool
				if err = func() error {
					defer row.Close()
					for row.Next() {
						var n, kind string
						if err = row.Scan(&n, &kind); err != nil {
							return err
						}
						entries = append(entries, struct{ n, kind string }{n: n, kind: kind})
					}
					return row.Err()
				}(); err != nil {
					return nil, err
				}
				for _, e := range entries {
					if e.kind == "m" && fullViewName != e.n {
						if _, err := tx.Exec(fmt.Sprintf("DROP MATERIALIZED VIEW %s CASCADE;", e.n)); err != nil {
							return nil, err
						}
					} else if e.kind == "v" && fullViewName != e.n {
						if _, err := tx.Exec(fmt.Sprintf("DROP VIEW %s CASCADE;", e.n)); err != nil {
							return nil, err
						}
					} else if e.kind == "r" && fullViewName != e.n {
						if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %s CASCADE;", e.n)); err != nil {
							return nil, err
						}
					} else if e.kind != "i" {
						exists = true
					}
				}

				if !exists {
					if _, err := tx.Exec(statement); err != nil {
						return nil, err
					}

					//Do first refresh
					if isMaterialized {
						if _, err := tx.Exec(fmt.Sprintf("REFRESH MATERIALIZED VIEW %s;", fullViewName)); err != nil {
							return nil, err
						}
					} else if isImmv {
						if _, err := tx.Exec(fmt.Sprintf("SELECT refresh_immv('%s', true);", fullViewName)); err != nil {
							return nil, err
						}
					}
					continue
				} else {
					continue
				}
			}

		}
		if _, err := tx.Exec(statement); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if index.statements.GetMinerById, err = index.handle.Prepare("SELECT " + MinerSelectFields + " FROM miners WHERE id = $1;"); err != nil {
		return nil, err
	}
	if index.statements.GetMinerByAddress, err = index.handle.Prepare("SELECT " + MinerSelectFields + " FROM miners WHERE spend_public_key = $1 AND view_public_key = $2;"); err != nil {
		return nil, err
	}
	if index.statements.GetMinerByAlias, err = index.handle.Prepare("SELECT " + MinerSelectFields + " FROM miners WHERE alias = $1;"); err != nil {
		return nil, err
	}
	if index.statements.InsertMiner, err = index.handle.Prepare("INSERT INTO miners (spend_public_key, view_public_key) VALUES ($1, $2) RETURNING " + MinerSelectFields + ";"); err != nil {
		return nil, err
	}
	if index.statements.TipSideBlocksTemplateId, err = index.PrepareSideBlocksByQueryStatement("WHERE template_id = $1 AND effective_height = side_height AND inclusion = $2;"); err != nil {
		return nil, err
	}

	if index.statements.InsertOrUpdateSideBlock, err = index.handle.Prepare("INSERT INTO side_blocks (" + SideBlockSelectFields + ") VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21) ON CONFLICT (main_id) DO UPDATE SET uncle_of = $7, effective_height = $8, inclusion = $21;"); err != nil {
		return nil, err
	}

	if index.statements.GetMainBlockByHeight, err = index.PrepareMainBlocksByQueryStatement("WHERE height = $1;"); err != nil {
		return nil, err
	}

	if index.statements.GetMainBlockById, err = index.PrepareMainBlocksByQueryStatement("WHERE id = $1;"); err != nil {
		return nil, err
	}

	if index.statements.GetSideBlockByMainId, err = index.PrepareSideBlocksByQueryStatement("WHERE main_id = $1;"); err != nil {
		return nil, err
	}

	if index.statements.GetSideBlockByUncleId, err = index.PrepareSideBlocksByQueryStatement("WHERE uncle_of = $1;"); err != nil {
		return nil, err
	}

	if index.statements.TipSideBlock, err = index.PrepareSideBlocksByQueryStatement("WHERE inclusion = $1 ORDER BY side_height DESC LIMIT 1;"); err != nil {
		return nil, err
	}

	if index.statements.TipMainBlock, err = index.PrepareMainBlocksByQueryStatement("ORDER BY height DESC LIMIT 1;"); err != nil {
		return nil, err
	}

	index.caches.miner = make(map[uint64]*Miner)

	return index, nil
}

func (i *Index) GetDifficultyByHeight(height uint64) types.Difficulty {
	if mb := i.GetMainBlockByHeight(height); mb != nil {
		return types.DifficultyFrom64(mb.Difficulty)
	}
	return i.getDifficultyByHeight(height)
}

func (i *Index) GetSeedByHeight(height uint64) types.Hash {
	seedHeight := randomx.SeedHeight(height)
	if mb := i.GetMainBlockByHeight(seedHeight); mb != nil {
		return mb.Id
	}
	return i.getSeedByHeight(height)
}

func (i *Index) GetByTemplateId(id types.Hash) *sidechain.PoolBlock {
	if v := i.blockCache.Get(id); v != nil {
		return *v
	} else {
		if b := i.getByTemplateId(id); b != nil {
			i.blockCache.Set(id, b)
			return b
		}
	}

	return nil
}

func (i *Index) CachePoolBlock(b *sidechain.PoolBlock) {
	i.blockCache.Set(b.SideTemplateId(i.consensus), b)
}

func (i *Index) Consensus() *sidechain.Consensus {
	return i.consensus
}

func (i *Index) GetMiner(miner uint64) *Miner {
	if m := func() *Miner {
		i.caches.minerLock.RLock()
		defer i.caches.minerLock.RUnlock()
		return i.caches.miner[miner]
	}(); m != nil {
		return m
	} else if m = i.getMiner(miner); m != nil {
		i.caches.minerLock.Lock()
		defer i.caches.minerLock.Unlock()
		i.caches.miner[miner] = m
		return m
	} else {
		return nil
	}
}

func (i *Index) getMiner(miner uint64) *Miner {
	if rows, err := i.statements.GetMinerById.Query(miner); err != nil {
		return nil
	} else {
		defer rows.Close()
		return i.scanMiner(rows)
	}
}

func (i *Index) GetMinerByAlias(alias string) *Miner {
	if rows, err := i.statements.GetMinerByAlias.Query(alias); err != nil {
		return nil
	} else {
		defer rows.Close()
		return i.scanMiner(rows)
	}
}

func (i *Index) GetMinerByStringAddress(addr string) *Miner {
	minerAddr := address.FromBase58(addr)
	if minerAddr != nil {
		return i.GetMinerByAddress(minerAddr)
	}
	return nil
}

func (i *Index) GetMinerByAddress(addr *address.Address) *Miner {
	if network, _ := i.consensus.NetworkType.AddressNetwork(); network != addr.Network {
		return nil
	}

	spendPub, viewPub := addr.SpendPublicKey().AsBytes(), addr.ViewPublicKey().AsBytes()
	if rows, err := i.statements.GetMinerByAddress.Query(&spendPub, &viewPub); err != nil {
		return nil
	} else {
		defer rows.Close()
		return i.scanMiner(rows)
	}
}

func (i *Index) GetMinerByPackedAddress(addr address.PackedAddress) *Miner {
	if rows, err := i.statements.GetMinerByAddress.Query(&addr[0], &addr[1]); err != nil {
		return nil
	} else {
		defer rows.Close()
		return i.scanMiner(rows)
	}
}

func (i *Index) GetOrCreateMinerByAddress(addr *address.Address) *Miner {
	if m := i.GetMinerByAddress(addr); m != nil {
		return m
	} else {
		spendPub, viewPub := addr.SpendPublicKey().AsSlice(), addr.ViewPublicKey().AsSlice()
		if rows, err := i.statements.InsertMiner.Query(&spendPub, &viewPub); err != nil {
			return nil
		} else {
			defer rows.Close()
			return i.scanMiner(rows)
		}
	}
}

func (i *Index) GetOrCreateMinerPackedAddress(addr address.PackedAddress) *Miner {
	if m := i.GetMinerByPackedAddress(addr); m != nil {
		return m
	} else {
		if rows, err := i.statements.InsertMiner.Query(&addr[0], &addr[1]); err != nil {
			return nil
		} else {
			defer rows.Close()
			return i.scanMiner(rows)
		}
	}
}

func (i *Index) SetMinerAlias(minerId uint64, alias string) error {
	miner := i.GetMiner(minerId)
	if miner == nil {
		return nil
	}
	if alias == "" {
		if err := i.Query("UPDATE miners SET alias = NULL WHERE id = $1;", nil, miner.Id()); err != nil {
			return err
		}
		miner.alias.String = ""
		miner.alias.Valid = false
	} else {
		if err := i.Query("UPDATE miners SET alias = $2 WHERE id = $1;", nil, miner.Id(), alias); err != nil {
			return err
		}
		miner.alias.String = alias
		miner.alias.Valid = true
	}

	return nil
}

type RowScanInterface interface {
	Scan(dest ...any) error
}

type Scannable interface {
	ScanFromRow(i *Index, row RowScanInterface) error
}

func (i *Index) GetView(k string) string {
	return i.views[k]
}

func (i *Index) Query(query string, callback func(row RowScanInterface) error, params ...any) error {
	var parentError error
	pprof.Do(context.Background(), pprof.Labels("query", query), func(ctx context.Context) {
		if stmt, err := i.handle.Prepare(query); err != nil {
			parentError = err
		} else {
			defer stmt.Close()
			parentError = i.QueryStatement(stmt, callback, params...)
		}
	})
	return parentError
}

func (i *Index) QueryStatement(stmt *sql.Stmt, callback func(row RowScanInterface) error, params ...any) error {
	if rows, err := stmt.Query(params...); err != nil {
		return err
	} else {
		defer rows.Close()
		for callback != nil && rows.Next() {
			//callback will call sql.Rows.Scan
			if err = callback(rows); err != nil {
				return err
			}
		}

		return nil
	}
}

func (i *Index) PrepareSideBlocksByQueryStatement(where string) (stmt *sql.Stmt, err error) {
	return i.handle.Prepare(fmt.Sprintf("SELECT "+SideBlockSelectFields+" FROM side_blocks %s;", where))
}

func (i *Index) GetSideBlocksByQuery(where string, params ...any) chan *SideBlock {
	if stmt, err := i.PrepareSideBlocksByQueryStatement(where); err != nil {
		returnChannel := make(chan *SideBlock, 1)
		close(returnChannel)
		return returnChannel
	} else {
		return i.getSideBlocksByQueryStatement(where, stmt, params...)
	}
}

func (i *Index) getSideBlocksByQueryStatement(sourceQuery string, stmt *sql.Stmt, params ...any) chan *SideBlock {
	returnChannel := make(chan *SideBlock, 1)
	go func() {
		defer stmt.Close()
		defer close(returnChannel)

		pprof.Do(context.Background(), pprof.Labels("sourceQuery", sourceQuery), func(ctx context.Context) {
			err := i.QueryStatement(stmt, func(row RowScanInterface) (err error) {
				b := &SideBlock{}
				if err = b.ScanFromRow(i, row); err != nil {
					return err
				}

				returnChannel <- b

				return nil
			}, params...)
			if err != nil {
				log.Print(err)
			}
		})
	}()

	return returnChannel
}

func (i *Index) GetSideBlocksByQueryStatement(source string, stmt *sql.Stmt, params ...any) chan *SideBlock {
	returnChannel := make(chan *SideBlock, 1)
	go func() {
		defer close(returnChannel)
		pprof.Do(context.Background(), pprof.Labels("source", source), func(ctx context.Context) {
			err := i.QueryStatement(stmt, func(row RowScanInterface) (err error) {
				b := &SideBlock{}
				if err = b.ScanFromRow(i, row); err != nil {
					return err
				}

				returnChannel <- b

				return nil
			}, params...)
			if err != nil {
				log.Print(err)
			}
		})
	}()

	return returnChannel
}

func (i *Index) PrepareMainBlocksByQueryStatement(where string) (stmt *sql.Stmt, err error) {
	return i.handle.Prepare(fmt.Sprintf("SELECT "+MainBlockSelectFields+" FROM main_blocks %s;", where))
}

func (i *Index) GetShares(limit, minerId uint64, onlyBlocks bool, inclusion BlockInclusion) chan *SideBlock {
	if limit == 0 {
		if minerId != 0 {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE miner = $1 AND uncle_of IS NULL AND inclusion = $2 ORDER BY side_height DESC;", minerId, inclusion)
			} else {
				return i.GetSideBlocksByQuery("WHERE miner = $1 AND inclusion = $2  ORDER BY side_height DESC, timestamp DESC;", minerId, inclusion)
			}
		} else {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE uncle_of IS NULL AND inclusion = $1 ORDER BY side_height DESC;", inclusion)
			} else {
				return i.GetSideBlocksByQuery("ORDER BY side_height AND inclusion = $1 DESC, timestamp DESC;", inclusion)
			}
		}
	} else {
		if minerId != 0 {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE miner = $1 AND uncle_of IS NULL AND inclusion = $3 ORDER BY side_height DESC LIMIT $2;", minerId, limit, inclusion)
			} else {
				return i.GetSideBlocksByQuery("WHERE miner = $1 AND inclusion = $3 ORDER BY side_height DESC, timestamp DESC LIMIT $2;", minerId, limit, inclusion)
			}
		} else {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE uncle_of IS NULL AND inclusion = $2 ORDER BY side_height DESC LIMIT $1;", limit, inclusion)
			} else {
				return i.GetSideBlocksByQuery("WHERE inclusion = $2 ORDER BY side_height DESC, timestamp DESC LIMIT $1;", limit, inclusion)
			}
		}
	}
}

func (i *Index) GetFoundBlocks(where string, limit uint64, params ...any) []*FoundBlock {
	result := make([]*FoundBlock, 0, limit)
	if err := i.Query(fmt.Sprintf("SELECT * FROM "+i.views["found_main_blocks"]+" %s ORDER BY main_height DESC LIMIT %d;", where, limit), func(row RowScanInterface) error {
		var d FoundBlock

		if err := d.ScanFromRow(i, row); err != nil {
			return err
		}

		result = append(result, &d)
		return nil
	}, params...); err != nil {
		return nil
	}
	return result
}

func (i *Index) GetMainBlocksByQuery(where string, params ...any) chan *MainBlock {
	if stmt, err := i.PrepareMainBlocksByQueryStatement(where); err != nil {
		returnChannel := make(chan *MainBlock, 1)
		close(returnChannel)
		return returnChannel
	} else {
		return i.getMainBlocksByQueryStatement(stmt, params...)
	}
}

func (i *Index) getMainBlocksByQueryStatement(stmt *sql.Stmt, params ...any) chan *MainBlock {
	returnChannel := make(chan *MainBlock, 1)
	go func() {
		defer stmt.Close()
		defer close(returnChannel)
		err := i.QueryStatement(stmt, func(row RowScanInterface) (err error) {
			b := &MainBlock{}
			if err = b.ScanFromRow(i, row); err != nil {
				return err
			}

			returnChannel <- b

			return nil
		}, params...)
		if err != nil {
			log.Print(err)
		}
	}()

	return returnChannel
}

func (i *Index) GetMainBlocksByQueryStatement(stmt *sql.Stmt, params ...any) chan *MainBlock {
	returnChannel := make(chan *MainBlock, 1)
	go func() {
		defer close(returnChannel)
		err := i.QueryStatement(stmt, func(row RowScanInterface) (err error) {
			b := &MainBlock{}
			if err = b.ScanFromRow(i, row); err != nil {
				return err
			}

			returnChannel <- b

			return nil
		}, params...)
		if err != nil {
			log.Print(err)
		}
	}()

	return returnChannel
}

func (i *Index) GetMainBlockById(id types.Hash) *MainBlock {
	r := i.GetMainBlocksByQueryStatement(i.statements.GetMainBlockById, id[:])
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetMainBlockTip() *MainBlock {
	r := i.GetMainBlocksByQueryStatement(i.statements.TipMainBlock)
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetMainBlockByCoinbaseId(id types.Hash) *MainBlock {
	r := i.GetMainBlocksByQuery("WHERE coinbase_id = $1;", id[:])
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetMainBlockByGlobalOutputIndex(globalOutputIndex uint64) *MainBlock {
	r := i.GetMainBlocksByQuery("WHERE coinbase_id = (SELECT id FROM main_coinbase_outputs WHERE global_output_index = $1 LIMIT 1);", globalOutputIndex)
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetMainBlockByHeight(height uint64) *MainBlock {
	r := i.GetMainBlocksByQueryStatement(i.statements.GetMainBlockByHeight, height)
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetSideBlockByMainId(id types.Hash) *SideBlock {
	r := i.GetSideBlocksByQueryStatement("GetSideBlockByMainId", i.statements.GetSideBlockByMainId, id[:])
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetSideBlocksByTemplateId(id types.Hash) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE template_id = $1;", id[:])
}

func (i *Index) GetSideBlocksByUncleOfId(id types.Hash) chan *SideBlock {
	return i.GetSideBlocksByQueryStatement("GetSideBlocksByUncleOfId", i.statements.GetSideBlockByUncleId, id[:])
}

func (i *Index) GetTipSideBlockByTemplateId(id types.Hash) *SideBlock {
	r := i.GetSideBlocksByQueryStatement("GetTipSideBlockByTemplateId", i.statements.TipSideBlocksTemplateId, id[:], InclusionInVerifiedChain)
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetSideBlocksByMainHeight(height uint64) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE main_height = $1;", height)
}

func (i *Index) GetSideBlocksByHeight(height uint64) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE side_height = $1;", height)
}

func (i *Index) GetTipSideBlockByHeight(height uint64) *SideBlock {
	r := i.GetSideBlocksByQuery("WHERE side_height = $1 AND effective_height = $2 AND inclusion = $3;", height, height, InclusionInVerifiedChain)
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetSideBlockTip() *SideBlock {
	r := i.GetSideBlocksByQueryStatement("GetSideBlockTip", i.statements.TipSideBlock, InclusionInVerifiedChain)
	defer ChanConsume(r)
	return <-r
}

func (i *Index) GetSideBlocksInPPLNSWindow(tip *SideBlock) chan *SideBlock {
	return i.GetSideBlocksInWindow(tip.SideHeight, uint64(tip.WindowDepth))
}

func (i *Index) GetSideBlocksInWindow(startHeight, windowSize uint64) chan *SideBlock {
	if startHeight < windowSize {
		windowSize = startHeight
	}
	return i.GetSideBlocksByQuery("WHERE effective_height <= $1 AND effective_height > $2 AND inclusion = $3 ORDER BY effective_height DESC, side_height DESC;", startHeight, startHeight-windowSize, InclusionInVerifiedChain)
}

func (i *Index) GetSideBlocksByMinerIdInWindow(minerId, startHeight, windowSize uint64) chan *SideBlock {
	if startHeight < windowSize {
		windowSize = startHeight
	}
	return i.GetSideBlocksByQuery("WHERE miner = $1 AND effective_height <= $2 AND effective_height > $3 AND inclusion = $4 ORDER BY effective_height DESC, side_height DESC;", minerId, startHeight, startHeight-windowSize, InclusionInVerifiedChain)
}

func (i *Index) InsertOrUpdateSideBlock(b *SideBlock) error {
	if b.IsTipOfHeight() {
		if oldBlock := i.GetTipSideBlockByHeight(b.SideHeight); oldBlock != nil {
			if oldBlock.MainId != b.MainId {
				//conflict resolution, change other block status
				if oldBlock.TemplateId == oldBlock.TemplateId {
					oldBlock.Inclusion = InclusionAlternateInVerifiedChain
				} else {
					//mark as orphan if templates don't match. if uncle it will be adjusted later
					oldBlock.Inclusion = InclusionOrphan
				}
				if err := i.InsertOrUpdateSideBlock(oldBlock); err != nil {
					return err
				}
			}
		}
	}

	var parentId any = &b.ParentTemplateId
	if b.SideHeight == 0 {
		parentId = types.ZeroHash[:]
	}

	return i.QueryStatement(
		i.statements.InsertOrUpdateSideBlock,
		nil,
		&b.MainId,
		b.MainHeight,
		&b.TemplateId,
		b.SideHeight,
		parentId,
		b.Miner,
		&b.UncleOf,
		b.EffectiveHeight,
		b.Nonce,
		b.ExtraNonce,
		b.Timestamp,
		b.SoftwareId,
		b.SoftwareVersion,
		b.WindowDepth,
		b.WindowOutputs,
		b.TransactionCount,
		b.Difficulty,
		&b.CumulativeDifficulty,
		b.PowDifficulty,
		&b.PowHash,
		b.Inclusion,
	)
}

func (i *Index) InsertOrUpdateMainBlock(b *MainBlock) error {
	if oldBlock := i.GetMainBlockByHeight(b.Height); oldBlock != nil {
		if oldBlock.Id != b.Id {
			//conflict resolution
			if tx, err := i.handle.BeginTx(context.Background(), nil); err != nil {
				return err
			} else {
				defer tx.Rollback()
				if _, err := tx.Exec("DELETE FROM main_coinbase_outputs WHERE id = $1;", oldBlock.CoinbaseId[:]); err != nil {
					return err
				}

				if _, err = tx.Exec("DELETE FROM main_blocks WHERE id = $1;", oldBlock.Id[:]); err != nil {
					return err
				}

				if err = tx.Commit(); err != nil {
					return err
				}
			}
		}
	}

	metadataJson, _ := utils.MarshalJSON(b.Metadata)

	if tx, err := i.handle.BeginTx(context.Background(), nil); err != nil {
		return err
	} else {
		defer tx.Rollback()
		if _, err := tx.Exec(
			"INSERT INTO main_blocks (id, height, timestamp, reward, coinbase_id, difficulty, metadata, side_template_id, coinbase_private_key) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9) ON CONFLICT (id) DO UPDATE SET metadata = $7, side_template_id = $8, coinbase_private_key = $9;",
			b.Id[:],
			b.Height,
			b.Timestamp,
			b.Reward,
			b.CoinbaseId[:],
			b.Difficulty,
			metadataJson,
			&b.SideTemplateId,
			&b.CoinbasePrivateKey,
		); err != nil {
			return err
		}

		return tx.Commit()
	}
}

func (i *Index) GetPayoutsByMinerId(minerId uint64, limit uint64) chan *Payout {
	out := make(chan *Payout, 1)

	go func() {
		defer close(out)

		resultFunc := func(row RowScanInterface) error {
			p := &Payout{}
			if err := p.ScanFromRow(i, row); err != nil {
				return err
			}
			out <- p
			return nil
		}

		if limit == 0 {
			if err := i.Query("SELECT * FROM "+i.views["payouts"]+" WHERE miner = $1 ORDER BY main_height DESC;", resultFunc, minerId); err != nil {
				return
			}
		} else {
			if err := i.Query("SELECT * FROM "+i.views["payouts"]+" WHERE miner = $1 ORDER BY main_height DESC LIMIT $2;", resultFunc, minerId, limit); err != nil {
				return
			}
		}
	}()

	return out
}

func (i *Index) GetPayoutsByMinerIdFromHeight(minerId uint64, height uint64) chan *Payout {
	out := make(chan *Payout, 1)

	go func() {
		defer close(out)

		resultFunc := func(row RowScanInterface) error {
			p := &Payout{}
			if err := p.ScanFromRow(i, row); err != nil {
				return err
			}
			out <- p
			return nil
		}

		if err := i.Query("SELECT * FROM "+i.views["payouts"]+" WHERE miner = $1 AND main_height >= $2 ORDER BY main_height DESC;", resultFunc, minerId, height); err != nil {
			return
		}
	}()

	return out
}

func (i *Index) GetPayoutsByMinerIdFromTimestamp(minerId uint64, timestamp uint64) chan *Payout {
	out := make(chan *Payout, 1)

	go func() {
		defer close(out)

		resultFunc := func(row RowScanInterface) error {
			p := &Payout{}
			if err := p.ScanFromRow(i, row); err != nil {
				return err
			}
			out <- p
			return nil
		}

		if err := i.Query("SELECT * FROM "+i.views["payouts"]+" WHERE miner = $1 AND timestamp >= $2 ORDER BY main_height DESC;", resultFunc, minerId, timestamp); err != nil {
			return
		}
	}()

	return out
}

func (i *Index) GetPayoutsBySideBlock(b *SideBlock) chan *Payout {
	out := make(chan *Payout, 1)

	go func() {
		defer close(out)

		resultFunc := func(row RowScanInterface) error {
			p := &Payout{}
			if err := p.ScanFromRow(i, row); err != nil {
				return err
			}
			out <- p
			return nil
		}

		if err := i.Query("SELECT * FROM "+i.views["payouts"]+" WHERE miner = $1 AND ((side_height >= $2 AND including_height <= $2) OR main_id = $3) ORDER BY main_height DESC;", resultFunc, b.Miner, b.EffectiveHeight, &b.MainId); err != nil {
			return
		}
	}()

	return out
}

func (i *Index) GetMainCoinbaseOutputs(coinbaseId types.Hash) MainCoinbaseOutputs {
	var outputs MainCoinbaseOutputs
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE id = $1 ORDER BY index ASC;", func(row RowScanInterface) error {
		var output MainCoinbaseOutput
		if err := output.ScanFromRow(i, row); err != nil {
			return err
		}
		outputs = append(outputs, output)
		return nil
	}, coinbaseId[:]); err != nil {
		return nil
	}
	return outputs
}

func (i *Index) GetMainCoinbaseOutputByIndex(coinbaseId types.Hash, index uint64) *MainCoinbaseOutput {
	var output MainCoinbaseOutput
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE id = $1 AND index = $2 ORDER BY index ASC;", func(row RowScanInterface) error {
		if err := output.ScanFromRow(i, row); err != nil {
			return err
		}
		return nil
	}, coinbaseId[:], index); err != nil {
		return nil
	}
	if output.Id == types.ZeroHash {
		return nil
	}
	return &output
}

func (i *Index) GetMainCoinbaseOutputByGlobalOutputIndex(globalOutputIndex uint64) *MainCoinbaseOutput {
	var output MainCoinbaseOutput
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE global_output_index = $1 ORDER BY index ASC;", func(row RowScanInterface) error {
		if err := output.ScanFromRow(i, row); err != nil {
			return err
		}
		return nil
	}, globalOutputIndex); err != nil {
		return nil
	}
	if output.Id == types.ZeroHash {
		return nil
	}
	return &output
}

func (i *Index) GetMainLikelySweepTransactions(limit uint64) chan *MainLikelySweepTransaction {
	out := make(chan *MainLikelySweepTransaction, 1)

	go func() {
		defer close(out)
		scanFunc := func(row RowScanInterface) error {
			var tx MainLikelySweepTransaction
			if err := tx.ScanFromRow(i, row); err != nil {
				return err
			}
			out <- &tx
			return nil
		}

		if limit > 0 {
			if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions ORDER BY timestamp DESC LIMIT $1;", scanFunc, limit); err != nil {
				return
			}
		} else {
			if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions ORDER BY timestamp DESC;", scanFunc); err != nil {
				return
			}
		}
	}()

	return out
}

func (i *Index) GetMainLikelySweepTransactionsByAddress(addr *address.Address, limit uint64) chan *MainLikelySweepTransaction {
	out := make(chan *MainLikelySweepTransaction, 1)

	go func() {
		defer close(out)

		spendPub, viewPub := addr.SpendPublicKey().AsSlice(), addr.ViewPublicKey().AsSlice()
		scanFunc := func(row RowScanInterface) error {
			var tx MainLikelySweepTransaction
			if err := tx.ScanFromRow(i, row); err != nil {
				return err
			}
			out <- &tx
			return nil
		}

		if limit > 0 {
			if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions WHERE miner_spend_public_key = $1 AND miner_view_public_key = $2 ORDER BY timestamp DESC LIMIT $3;", scanFunc, &spendPub, &viewPub, limit); err != nil {
				return
			}
		} else {
			if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions WHERE miner_spend_public_key = $1 AND miner_view_public_key = $2 ORDER BY timestamp DESC;", scanFunc, &spendPub, &viewPub); err != nil {
				return
			}
		}
	}()

	return out
}

type TransactionInputQueryResult struct {
	Input          client.TransactionInput `json:"input"`
	MatchedOutputs []*MatchedOutput        `json:"matched_outputs"`
}

type MatchedOutput struct {
	Coinbase          *MainCoinbaseOutput         `json:"coinbase,omitempty"`
	Sweep             *MainLikelySweepTransaction `json:"sweep,omitempty"`
	GlobalOutputIndex uint64                      `json:"global_output_index"`
	Timestamp         uint64                      `json:"timestamp"`
	Address           *address.Address            `json:"address"`
}

type MinimalTransactionInputQueryResult struct {
	Input          client.TransactionInput `json:"input"`
	MatchedOutputs []*MinimalMatchedOutput `json:"matched_outputs"`
}

type MinimalMatchedOutput struct {
	Coinbase          types.Hash       `json:"coinbase,omitempty"`
	Sweep             types.Hash       `json:"sweep,omitempty"`
	GlobalOutputIndex uint64           `json:"global_output_index"`
	Address           *address.Address `json:"address"`
}

type MinimalTransactionInputQueryResults []MinimalTransactionInputQueryResult
type TransactionInputQueryResults []TransactionInputQueryResult

type TransactionInputQueryResultsMatch struct {
	Address        *address.Address `json:"address"`
	Count          uint64           `json:"count"`
	SweepCount     uint64           `json:"sweep_count"`
	CoinbaseCount  uint64           `json:"coinbase_count"`
	CoinbaseAmount uint64           `json:"coinbase_amount"`
}
type TransactionInputQueryResultsMatches []TransactionInputQueryResultsMatch

func (r TransactionInputQueryResults) Match() (result TransactionInputQueryResultsMatches) {
	//cannot have more than one of same miner outputs valid per input
	//no miner outputs in whole input doesn't count
	//cannot take vsame exact miner outputs on different inputs
	//TODO

	var zeroAddress address.PackedAddress
	miners := make(map[address.PackedAddress]*TransactionInputQueryResultsMatch)
	miners[zeroAddress] = &TransactionInputQueryResultsMatch{
		Address:        nil,
		Count:          0,
		SweepCount:     0,
		CoinbaseCount:  0,
		CoinbaseAmount: 0,
	}

	//TODO: handle same miner multiple times in same decoy

	for _, matchResult := range r {
		for _, o := range matchResult.MatchedOutputs {
			if o != nil {
				pA := o.Address.ToPackedAddress()
				if _, ok := miners[pA]; !ok {
					miners[pA] = &TransactionInputQueryResultsMatch{
						Address:        o.Address,
						Count:          0,
						SweepCount:     0,
						CoinbaseCount:  0,
						CoinbaseAmount: 0,
					}
				}

				if o.Coinbase != nil {
					miners[pA].CoinbaseCount++
					miners[pA].CoinbaseAmount += o.Coinbase.Value
				} else if o.Sweep != nil {
					miners[pA].SweepCount++
				}
				miners[pA].Count++
			} else {
				miners[zeroAddress].Count++
			}
		}
	}

	result = make([]TransactionInputQueryResultsMatch, 0, len(miners))

	for _, v := range miners {
		result = append(result, *v)
	}

	slices.SortFunc(result, func(a, b TransactionInputQueryResultsMatch) bool {
		return a.Count > b.Count
	})

	return result
}

func (i *Index) QueryTransactionInputs(inputs []client.TransactionInput) TransactionInputQueryResults {
	result := make(TransactionInputQueryResults, len(inputs))
	for index, input := range inputs {
		result[index].Input = input
		result[index].MatchedOutputs = make([]*MatchedOutput, len(input.KeyOffsets))
		if input.Amount != 0 {
			continue
		}
		result[index].MatchedOutputs = i.QueryGlobalOutputIndices(input.KeyOffsets)
	}
	return result
}

func (i *Index) QueryGlobalOutputIndices(indices []uint64) []*MatchedOutput {
	result := make([]*MatchedOutput, len(indices))

	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE global_output_index = ANY($1) ORDER BY index ASC;", func(row RowScanInterface) error {
		var o MainCoinbaseOutput
		if err := o.ScanFromRow(i, row); err != nil {
			return err
		}
		if index := slices.Index(indices, o.GlobalOutputIndex); index != -1 {
			result[index] = &MatchedOutput{
				Coinbase:          &o,
				Sweep:             nil,
				GlobalOutputIndex: o.GlobalOutputIndex,
				Address:           i.GetMiner(o.Miner).Address(),
			}
			if mb := i.GetMainBlockByCoinbaseId(o.Id); mb != nil {
				result[index].Timestamp = mb.Timestamp
			}
		}
		return nil
	}, pq.Array(indices)); err != nil {
		return nil
	}

	if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions WHERE $1::bigint[] && global_output_indices ORDER BY timestamp ASC;", func(row RowScanInterface) error {
		var tx MainLikelySweepTransaction
		if err := tx.ScanFromRow(i, row); err != nil {
			return err
		}
		for _, globalOutputIndex := range tx.GlobalOutputIndices {
			// fill all possible indices
			if index := slices.Index(indices, globalOutputIndex); index != -1 {
				if result[index] == nil {
					result[index] = &MatchedOutput{
						Coinbase:          nil,
						Sweep:             &tx,
						GlobalOutputIndex: globalOutputIndex,
						Timestamp:         tx.Timestamp,
						Address:           tx.Address,
					}
				}
			}
		}
		return nil
	}, pq.Array(indices)); err != nil {
		return nil
	}
	return result
}

func (i *Index) GetMainLikelySweepTransactionBySpendingGlobalOutputIndices(globalOutputIndices ...uint64) [][]*MainLikelySweepTransaction {
	entries := make([][]*MainLikelySweepTransaction, len(globalOutputIndices))
	if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions WHERE $1::bigint[] && spending_output_indices ORDER BY timestamp ASC;", func(row RowScanInterface) error {
		var tx MainLikelySweepTransaction
		if err := tx.ScanFromRow(i, row); err != nil {
			return err
		}
		for _, globalOutputIndex := range tx.SpendingOutputIndices {
			// fill all possible indices
			if index := slices.Index(globalOutputIndices, globalOutputIndex); index != -1 {
				entries[index] = append(entries[index], &tx)
			}
		}
		return nil
	}, pq.Array(globalOutputIndices)); err != nil {
		return nil
	}
	return entries
}

func (i *Index) GetMainLikelySweepTransactionByGlobalOutputIndices(globalOutputIndices ...uint64) []*MainLikelySweepTransaction {
	entries := make([]*MainLikelySweepTransaction, len(globalOutputIndices))
	if err := i.Query("SELECT "+MainLikelySweepTransactionSelectFields+" FROM main_likely_sweep_transactions WHERE $1::bigint[] && global_output_indices ORDER BY timestamp ASC;", func(row RowScanInterface) error {
		var tx MainLikelySweepTransaction
		if err := tx.ScanFromRow(i, row); err != nil {
			return err
		}
		for _, globalOutputIndex := range tx.GlobalOutputIndices {
			// fill all possible indices
			if index := slices.Index(globalOutputIndices, globalOutputIndex); index != -1 {
				if entries[index] == nil {
					entries[index] = &tx
				}
			}
		}
		return nil
	}, pq.Array(globalOutputIndices)); err != nil {
		return nil
	}
	return entries
}

func (i *Index) InsertOrUpdateMainLikelySweepTransaction(t *MainLikelySweepTransaction) error {

	resultJson, _ := utils.MarshalJSON(t.Result)
	matchJson, _ := utils.MarshalJSON(t.Match)
	spendPub, viewPub := t.Address.SpendPublicKey().AsSlice(), t.Address.ViewPublicKey().AsSlice()

	if _, err := i.handle.Exec(
		"INSERT INTO main_likely_sweep_transactions (id, timestamp, result, match, value, spending_output_indices, global_output_indices, input_count, input_decoy_count, miner_count, other_miners_count, no_miner_count, miner_ratio, other_miners_ratio, no_miner_ratio, miner_spend_public_key, miner_view_public_key) VALUES ($1, $2, $3, $4, $5, $6::bigint[], $7::bigint[], $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) ON CONFLICT (id) DO UPDATE SET result = $3, match = $4, value = $5, miner_count = $10, other_miners_count = $11, no_miner_count = $12, miner_ratio = $13, other_miners_ratio = $14, no_miner_ratio = $15, miner_spend_public_key = $16, miner_view_public_key = $17;",
		t.Id[:],
		t.Timestamp,
		resultJson,
		matchJson,
		t.Value,
		pq.Array(t.SpendingOutputIndices),
		pq.Array(t.GlobalOutputIndices),
		t.InputCount,
		t.InputDecoyCount,
		t.MinerCount,
		t.OtherMinersCount,
		t.NoMinerCount,
		t.MinerRatio,
		t.OtherMinersRatio,
		t.NoMinerRatio,
		&spendPub,
		&viewPub,
	); err != nil {
		return err
	}

	return nil
}

func (i *Index) GetMainCoinbaseOutputByMinerId(coinbaseId types.Hash, minerId uint64) *MainCoinbaseOutput {
	var output MainCoinbaseOutput
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE id = $1 AND miner = $2 ORDER BY index DESC;", func(row RowScanInterface) error {
		if err := output.ScanFromRow(i, row); err != nil {
			return err
		}
		return nil
	}, coinbaseId[:], minerId); err != nil {
		return nil
	}
	if output.Id == types.ZeroHash {
		return nil
	}
	return &output
}

func (i *Index) InsertOrUpdateMainCoinbaseOutputs(outputs MainCoinbaseOutputs) error {
	if len(outputs) == 0 {
		return nil
	}
	for ix, o := range outputs[1:] {
		if outputs[ix].Id != o.Id {
			return errors.New("differing coinbase ids")
		}
	}

	if tx, err := i.handle.BeginTx(context.Background(), nil); err != nil {
		return err
	} else {
		defer tx.Rollback()
		for _, o := range outputs {
			if _, err := tx.Exec(
				"INSERT INTO main_coinbase_outputs (id, index, global_output_index, miner, value) VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING;",
				o.Id[:],
				o.Index,
				o.GlobalOutputIndex,
				o.Miner,
				o.Value,
			); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
}

func (i *Index) Close() error {
	//cleanup statements
	v := reflect.ValueOf(i.statements)
	for ix := 0; ix < v.NumField(); ix++ {
		if stmt, ok := v.Field(ix).Interface().(*sql.Stmt); ok && stmt != nil {
			//v.Field(i).Elem().Set(reflect.ValueOf((*sql.Stmt)(nil)))
			stmt.Close()
		}
	}

	return i.handle.Close()
}

func (i *Index) scanMiner(rows *sql.Rows) *Miner {
	if rows.Next() {
		m := &Miner{}
		if m.ScanFromRow(i, rows) == nil {
			return m
		}
	}
	return nil
}

func (i *Index) GetSideBlockFromPoolBlock(b *sidechain.PoolBlock, inclusion BlockInclusion) (tip *SideBlock, uncles []*SideBlock, err error) {
	if err = i.preProcessPoolBlock(b); err != nil {
		return nil, nil, err
	}
	tip = &SideBlock{}
	if err = tip.FromPoolBlock(i, b, i.GetSeedByHeight); err != nil {
		return nil, nil, err
	}
	tip.EffectiveHeight = tip.SideHeight
	tip.Inclusion = inclusion

	if bottomHeight, err := sidechain.BlocksInPPLNSWindow(b, i.consensus, i.GetDifficultyByHeight, i.GetByTemplateId, nil); err != nil {
		// unknown
		tip.WindowDepth = 0
	} else {
		tip.WindowDepth = uint32(tip.SideHeight - bottomHeight + 1)
	}

	for _, u := range b.Side.Uncles {
		uncleBlock := i.GetTipSideBlockByTemplateId(u)
		uncle := i.GetByTemplateId(u)
		if err = i.preProcessPoolBlock(uncle); err != nil {
			return nil, nil, err
		}
		if uncleBlock == nil && uncle != nil {
			uncleBlock = &SideBlock{}
			if err = uncleBlock.FromPoolBlock(i, uncle, i.GetSeedByHeight); err != nil {
				return nil, nil, err
			}
			if tip.Inclusion == InclusionOrphan {
				uncleBlock.Inclusion = InclusionOrphan
			} else if tip.Inclusion == InclusionInVerifiedChain || tip.Inclusion == InclusionAlternateInVerifiedChain {
				uncleBlock.Inclusion = InclusionInVerifiedChain
			}
		}
		if uncleBlock == nil || uncle == nil {
			return nil, nil, errors.New("nil uncle")
		}
		if tip.Inclusion == InclusionInVerifiedChain || tip.Inclusion == InclusionAlternateInVerifiedChain {
			uncleBlock.UncleOf = tip.TemplateId
			uncleBlock.EffectiveHeight = tip.EffectiveHeight
			uncleBlock.Inclusion = InclusionInVerifiedChain
		}

		if uncleBottomHeight, err := sidechain.BlocksInPPLNSWindow(uncle, i.consensus, i.GetDifficultyByHeight, i.GetByTemplateId, nil); err != nil {
			// unknown
			uncleBlock.WindowDepth = 0
		} else {
			uncleBlock.WindowDepth = uint32(uncleBlock.SideHeight - uncleBottomHeight + 1)
		}
		uncles = append(uncles, uncleBlock)
	}

	return tip, uncles, nil
}

func (i *Index) preProcessPoolBlock(b *sidechain.PoolBlock) error {
	if b == nil {
		return errors.New("nil block")
	}
	var preAllocatedShares sidechain.Shares
	if len(b.Main.Coinbase.Outputs) == 0 {
		//cannot use SideTemplateId() as it might not be proper to calculate yet. fetch from coinbase only here
		if b2 := i.GetByTemplateId(types.HashFromBytes(b.CoinbaseExtra(sidechain.SideTemplateId))); b2 != nil && len(b2.Main.Coinbase.Outputs) != 0 {
			b.Main.Coinbase.Outputs = b2.Main.Coinbase.Outputs
		} else {
			preAllocatedShares = sidechain.PreAllocateShares(i.consensus.ChainWindowSize * 2)
		}
	}

	_, err := b.PreProcessBlock(i.consensus, i.derivationCache, preAllocatedShares, i.GetDifficultyByHeight, i.GetByTemplateId)
	return err
}

func (i *Index) InsertOrUpdatePoolBlock(b *sidechain.PoolBlock, inclusion BlockInclusion) error {
	sideBlock, sideUncles, err := i.GetSideBlockFromPoolBlock(b, inclusion)
	if err != nil {
		return err
	}

	if err := i.InsertOrUpdateSideBlock(sideBlock); err != nil {
		return err
	}

	for _, sideUncle := range sideUncles {
		if err := i.InsertOrUpdateSideBlock(sideUncle); err != nil {
			return err
		}
	}

	return nil
}

func ChanConsume[T any](s chan T) {
	for range s {

	}
}

func ChanToSlice[T any](s chan T) (r []T) {
	r = make([]T, 0)
	for v := range s {
		r = append(r, v)
	}
	return r
}
