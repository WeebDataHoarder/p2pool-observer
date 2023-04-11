package index

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/floatdrop/lru"
	_ "github.com/lib/pq"
	"log"
	"reflect"
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
		TipSideBlocksTemplateId *sql.Stmt
		InsertOrUpdateSideBlock *sql.Stmt
	}
	caches struct {
		minerLock sync.RWMutex
		miner     map[uint64]*Miner
	}
}

//go:embed schema.sql
var dbSchema string

func OpenIndex(connStr string, consensus *sidechain.Consensus, difficultyByHeight block.GetDifficultyByHeightFunc, getSeedByHeight block.GetSeedByHeightFunc, getByTemplateId sidechain.GetByTemplateIdFunc) (index *Index, err error) {
	index = &Index{
		consensus:             consensus,
		getDifficultyByHeight: difficultyByHeight,
		getSeedByHeight:       getSeedByHeight,
		getByTemplateId:       getByTemplateId,
		derivationCache:       sidechain.NewDerivationCache(),
		blockCache:            lru.New[types.Hash, *sidechain.PoolBlock](int(consensus.ChainWindowSize * 4)),
	}
	if index.handle, err = sql.Open("postgres", connStr); err != nil {
		return nil, err
	}

	tx, err := index.handle.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, statement := range strings.Split(dbSchema, ";") {
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
	switch addr.Network {
	case moneroutil.MainNetwork:
		if i.consensus.NetworkType != sidechain.NetworkMainnet {
			return nil
		}
	case moneroutil.TestNetwork:
		if i.consensus.NetworkType != sidechain.NetworkTestnet {
			return nil
		}
	default:
		return nil
	}
	spendPub, viewPub := addr.SpendPub.AsBytes(), addr.ViewPub.AsBytes()
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
		spendPub, viewPub := addr.SpendPub.AsSlice(), addr.ViewPub.AsSlice()
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

func (i *Index) Query(query string, callback func(row RowScanInterface) error, params ...any) error {
	if stmt, err := i.handle.Prepare(query); err != nil {
		return err
	} else {
		defer stmt.Close()

		return i.QueryStatement(stmt, callback, params...)
	}
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
		returnChannel := make(chan *SideBlock)
		close(returnChannel)
		return returnChannel
	} else {
		return i.getSideBlocksByQueryStatement(stmt, params...)
	}
}

func (i *Index) getSideBlocksByQueryStatement(stmt *sql.Stmt, params ...any) chan *SideBlock {
	returnChannel := make(chan *SideBlock)
	go func() {
		defer stmt.Close()
		defer close(returnChannel)
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
	}()

	return returnChannel
}

func (i *Index) GetSideBlocksByQueryStatement(stmt *sql.Stmt, params ...any) chan *SideBlock {
	returnChannel := make(chan *SideBlock)
	go func() {
		defer close(returnChannel)
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
	}()

	return returnChannel
}

func (i *Index) PrepareMainBlocksByQueryStatement(where string) (stmt *sql.Stmt, err error) {
	return i.handle.Prepare(fmt.Sprintf("SELECT "+MainBlockSelectFields+" FROM main_blocks %s;", where))
}

type BlockFound struct {
	MainBlock MainBlock

	SideHeight           uint64
	Miner                uint64
	UncleOf              types.Hash
	WindowDepth          uint32
	WindowOutputs        uint32
	Difficulty           uint64
	CumulativeDifficulty types.Difficulty
	Inclusion            BlockInclusion
}

func (i *Index) GetShares(limit, minerId uint64, onlyBlocks bool) chan *SideBlock {
	if limit == 0 {
		if minerId != 0 {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE miner = $1 AND uncle_of IS NULL ORDER BY side_height DESC;", minerId)
			} else {
				return i.GetSideBlocksByQuery("WHERE miner = $1 ORDER BY side_height DESC, timestamp DESC;", minerId)
			}
		} else {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE uncle_of IS NULL ORDER BY side_height DESC;")
			} else {
				return i.GetSideBlocksByQuery("ORDER BY side_height DESC, timestamp DESC;")
			}
		}
	} else {
		if minerId != 0 {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE miner = $1 AND uncle_of IS NULL ORDER BY side_height DESC LIMIT $2;", minerId, limit)
			} else {
				return i.GetSideBlocksByQuery("WHERE miner = $1 ORDER BY side_height DESC, timestamp DESC LIMIT $2;", minerId, limit)
			}
		} else {
			if onlyBlocks {
				return i.GetSideBlocksByQuery("WHERE uncle_of IS NULL ORDER BY side_height DESC LIMIT $1;", limit)
			} else {
				return i.GetSideBlocksByQuery("ORDER BY side_height DESC, timestamp DESC LIMIT $1;", limit)
			}
		}
	}
}

func (i *Index) GetBlocksFound(where string, limit uint64, params ...any) []*BlockFound {
	result := make([]*BlockFound, 0, limit)
	if err := i.Query(fmt.Sprintf("SELECT m.id AS main_id, m.height AS main_height, m.timestamp AS timestamp, m.reward AS reward, m.coinbase_id AS coinbase_id, m.coinbase_private_key AS coinbase_private_key, m.difficulty AS main_difficulty, m.side_template_id AS template_id, s.side_height AS side_height, s.miner AS miner, s.uncle_of AS uncle_of, s.window_depth AS window_depth, s.window_outputs AS window_outputs, s.difficulty AS side_difficulty, s.cumulative_difficulty AS side_cumulative_difficulty, s.inclusion AS side_inclusion FROM (SELECT * FROM main_blocks WHERE side_template_id IS NOT NULL) AS m LEFT JOIN LATERAL (SELECT * FROM side_blocks WHERE main_id = m.id) s ON m.id = s.main_id %s ORDER BY main_height DESC LIMIT %d;", where, limit), func(row RowScanInterface) error {
		var d BlockFound

		if err := row.Scan(
			&d.MainBlock.Id, &d.MainBlock.Height, &d.MainBlock.Timestamp, &d.MainBlock.Reward, &d.MainBlock.CoinbaseId, &d.MainBlock.CoinbasePrivateKey, &d.MainBlock.Difficulty, &d.MainBlock.SideTemplateId,
			&d.SideHeight, &d.Miner, &d.UncleOf, &d.WindowDepth, &d.WindowOutputs, &d.Difficulty, &d.CumulativeDifficulty, &d.Inclusion,
		); err != nil {
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
		returnChannel := make(chan *MainBlock)
		close(returnChannel)
		return returnChannel
	} else {
		return i.getMainBlocksByQueryStatement(stmt, params...)
	}
}

func (i *Index) getMainBlocksByQueryStatement(stmt *sql.Stmt, params ...any) chan *MainBlock {
	returnChannel := make(chan *MainBlock)
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
	returnChannel := make(chan *MainBlock)
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
	r := i.GetMainBlocksByQuery("WHERE id = $1;", id[:])
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetMainBlockTip() *MainBlock {
	r := i.GetMainBlocksByQuery("ORDER BY height DESC LIMIT 1;")
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetMainBlockByHeight(height uint64) *MainBlock {
	r := i.GetMainBlocksByQuery("WHERE height = $1;", height)
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetSideBlockByMainId(id types.Hash) *SideBlock {
	r := i.GetSideBlocksByQuery("WHERE main_id = $1;", id[:])
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetSideBlocksByTemplateId(id types.Hash) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE template_id = $1;", id[:])
}

func (i *Index) GetSideBlocksByUncleId(id types.Hash) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE uncle_of = $1;", id[:])
}

func (i *Index) GetTipSideBlockByTemplateId(id types.Hash) *SideBlock {
	r := i.GetSideBlocksByQueryStatement(i.statements.TipSideBlocksTemplateId, id[:], InclusionInVerifiedChain)
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetSideBlocksByHeight(height uint64) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE side_height = $1;", height)
}

func (i *Index) GetTipSideBlockByHeight(height uint64) *SideBlock {
	r := i.GetSideBlocksByQuery("WHERE side_height = $1 AND effective_height = $2 AND inclusion = $3;", height, height, InclusionInVerifiedChain)
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetSideBlockTip() *SideBlock {
	//TODO: check performance of inclusion here
	r := i.GetSideBlocksByQuery("WHERE inclusion = $1 ORDER BY side_height DESC LIMIT 1;", InclusionInVerifiedChain)
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (i *Index) GetSideBlocksInPPLNSWindow(tip *SideBlock) chan *SideBlock {
	return i.GetSideBlocksByQuery("WHERE effective_height <= $1 AND effective_height > $2 AND inclusion = $3 ORDER BY effective_height DESC, side_height DESC;", tip.SideHeight, tip.SideHeight-uint64(tip.WindowDepth), InclusionInVerifiedChain)
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

	return i.QueryStatement(
		i.statements.InsertOrUpdateSideBlock,
		nil,
		&b.MainId,
		b.MainHeight,
		&b.TemplateId,
		b.SideHeight,
		&b.ParentTemplateId,
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

	metadataJson, _ := json.Marshal(b.Metadata)

	return i.Query(
		"INSERT INTO main_blocks (id, height, timestamp, reward, coinbase_id, difficulty, metadata, side_template_id, coinbase_private_key) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9) ON CONFLICT (id) DO UPDATE SET metadata = $7, side_template_id = $8, coinbase_private_key = $9;",
		nil,
		b.Id[:],
		b.Height,
		b.Timestamp,
		b.Reward,
		b.CoinbaseId[:],
		b.Difficulty,
		metadataJson,
		&b.SideTemplateId,
		&b.CoinbasePrivateKey,
	)
}

type Payout struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
	Main   struct {
		Id     types.Hash `json:"id"`
		Height uint64     `json:"height"`
	} `json:"main"`
	Timestamp uint64 `json:"timestamp"`
	Uncle     bool   `json:"uncle,omitempty"`
	Coinbase  struct {
		Id         types.Hash             `json:"id"`
		Reward     uint64                 `json:"reward"`
		PrivateKey crypto.PrivateKeyBytes `json:"private_key"`
		Index      uint64                 `json:"index"`
	} `json:"coinbase"`
}

func (i *Index) GetPayoutsByMinerId(minerId uint64, limit uint64) chan *Payout {
	out := make(chan *Payout)

	go func() {
		defer close(out)

		resultFunc := func(row RowScanInterface) error {
			var templateId, mainId, privKey, coinbaseId []byte
			var height, mainHeight, timestamp, value, index uint64
			var uncle []byte

			if err := row.Scan(&mainId, &mainHeight, &timestamp, &coinbaseId, &privKey, &templateId, &height, &uncle, &value, &index); err != nil {
				return err
			}

			out <- &Payout{
				Id:        types.HashFromBytes(templateId),
				Height:    height,
				Timestamp: timestamp,
				Main: struct {
					Id     types.Hash `json:"id"`
					Height uint64     `json:"height"`
				}{Id: types.HashFromBytes(mainId), Height: mainHeight},
				Uncle: len(uncle) != 0,
				Coinbase: struct {
					Id         types.Hash             `json:"id"`
					Reward     uint64                 `json:"reward"`
					PrivateKey crypto.PrivateKeyBytes `json:"private_key"`
					Index      uint64                 `json:"index"`
				}{Id: types.HashFromBytes(coinbaseId), Reward: value, PrivateKey: crypto.PrivateKeyBytes(types.HashFromBytes(privKey)), Index: index},
			}
			return nil
		}

		if limit == 0 {
			if err := i.Query("SELECT m.id AS main_id, m.height AS main_height, m.timestamp AS timestamp, m.coinbase_id AS coinbase_id, m.coinbase_private_key AS coinbase_private_key, m.side_template_id AS template_id, s.side_height AS side_height, s.uncle_of AS uncle_of, o.value AS value, o.index AS index FROM (SELECT id, value, index FROM main_coinbase_outputs WHERE miner = $1) o LEFT JOIN LATERAL (SELECT id, height, timestamp, side_template_id, coinbase_id, coinbase_private_key FROM main_blocks WHERE coinbase_id = o.id) m ON m.coinbase_id = o.id LEFT JOIN LATERAL (SELECT template_id, main_id, side_height, uncle_of FROM side_blocks WHERE main_id = m.id) s ON s.main_id = m.id ORDER BY main_height DESC;", resultFunc, minerId); err != nil {
				return
			}
		} else {
			if err := i.Query("SELECT m.id AS main_id, m.height AS main_height, m.timestamp AS timestamp, m.coinbase_id AS coinbase_id, m.coinbase_private_key AS coinbase_private_key, m.side_template_id AS template_id, s.side_height AS side_height, s.uncle_of AS uncle_of, o.value AS value, o.index AS index FROM (SELECT id, value, index FROM main_coinbase_outputs WHERE miner = $1) o LEFT JOIN LATERAL (SELECT id, height, timestamp, side_template_id, coinbase_id, coinbase_private_key FROM main_blocks WHERE coinbase_id = o.id) m ON m.coinbase_id = o.id LEFT JOIN LATERAL (SELECT template_id, main_id, side_height, uncle_of FROM side_blocks WHERE main_id = m.id) s ON s.main_id = m.id ORDER BY main_height DESC LIMIT $2;", resultFunc, minerId, limit); err != nil {
				return
			}
		}
	}()

	return out
}

func (i *Index) GetPayoutsBySideBlock(b *SideBlock) chan *Payout {
	out := make(chan *Payout)

	go func() {
		defer close(out)

		resultFunc := func(row RowScanInterface) error {
			var templateId, mainId, privKey, coinbaseId []byte
			var height, mainHeight, timestamp, value, index uint64
			var uncle []byte

			if err := row.Scan(&mainId, &mainHeight, &timestamp, &coinbaseId, &privKey, &templateId, &height, &uncle, &value, &index); err != nil {
				return err
			}

			out <- &Payout{
				Id:        types.HashFromBytes(templateId),
				Height:    height,
				Timestamp: timestamp,
				Main: struct {
					Id     types.Hash `json:"id"`
					Height uint64     `json:"height"`
				}{Id: types.HashFromBytes(mainId), Height: mainHeight},
				Uncle: len(uncle) != 0,
				Coinbase: struct {
					Id         types.Hash             `json:"id"`
					Reward     uint64                 `json:"reward"`
					PrivateKey crypto.PrivateKeyBytes `json:"private_key"`
					Index      uint64                 `json:"index"`
				}{Id: types.HashFromBytes(coinbaseId), Reward: value, PrivateKey: crypto.PrivateKeyBytes(types.HashFromBytes(privKey)), Index: index},
			}
			return nil
		}

		if err := i.Query("SELECT m.id AS main_id, m.height AS main_height, m.timestamp AS timestamp, m.coinbase_id AS coinbase_id, m.coinbase_private_key AS coinbase_private_key, m.side_template_id AS template_id, s.side_height AS side_height, s.uncle_of AS uncle_of, o.value AS value, o.index AS index FROM (SELECT id, value, index FROM main_coinbase_outputs WHERE miner = $1) o LEFT JOIN LATERAL (SELECT id, height, timestamp, side_template_id, coinbase_id, coinbase_private_key FROM main_blocks WHERE coinbase_id = o.id) m ON m.coinbase_id = o.id LEFT JOIN LATERAL (SELECT template_id, main_id, side_height, uncle_of, (effective_height - window_depth) AS including_height FROM side_blocks WHERE main_id = m.id) s ON s.main_id = m.id WHERE side_height >= $2 AND including_height <= $2 ORDER BY main_height DESC;", resultFunc, b.Miner, b.EffectiveHeight); err != nil {
			return
		}
	}()

	return out
}

func (i *Index) GetMainCoinbaseOutputs(coinbaseId types.Hash) MainCoinbaseOutputs {
	var outputs MainCoinbaseOutputs
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE id = $1 ORDER BY index DESC;", func(row RowScanInterface) error {
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
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE id = $1 AND index = $2 ORDER BY index DESC;", func(row RowScanInterface) error {
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
	if err := i.Query("SELECT "+MainCoinbaseOutputSelectFields+" FROM main_coinbase_outputs WHERE global_output_index = $1 ORDER BY index DESC;", func(row RowScanInterface) error {
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

type TransactionInputQueryResult struct {
	Input          client.TransactionInput
	MatchedOutputs []*MainCoinbaseOutput
}

type TransactionInputQueryResults []TransactionInputQueryResult

func (r TransactionInputQueryResults) Match() {
	//cannot have more than one of same miner outputs valid per input
	//no miner outputs in whole input doesn't count
	//cannot take same exact miner outputs on different inputs
	//TODO

	minerCountsTotal := make(map[uint64]uint64)
	for _, result := range r {
		minerCountsInInput := make(map[uint64]uint64)
		for _, o := range result.MatchedOutputs {
			if o != nil {
				minerCountsInInput[o.Miner]++
			}
		}
		for minerId, count := range minerCountsInInput {
			if count > 1 {
				minerCountsInInput[minerId] = 1
				//cannot have more than one of our outputs valid per input
			}
			minerCountsTotal[minerId]++
		}
	}
}

func (i *Index) QueryTransactionInputs(inputs []client.TransactionInput) TransactionInputQueryResults {
	result := make(TransactionInputQueryResults, len(inputs))
	for index, input := range inputs {
		result[index].MatchedOutputs = make([]*MainCoinbaseOutput, len(input.KeyOffsets))
		if input.Amount != 0 {
			continue
		}
		for ki, k := range input.KeyOffsets {
			//TODO: query many at once
			result[index].MatchedOutputs[ki] = i.GetMainCoinbaseOutputByGlobalOutputIndex(k)
		}
	}
	return result
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
			if _, err = tx.Exec(
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
	var bottomHeight uint64
	for e := range sidechain.IterateBlocksInPPLNSWindow(b, i.consensus, i.GetDifficultyByHeight, i.GetByTemplateId, nil, func(err error) {
		bottomHeight = 0
	}) {
		bottomHeight = e.Block.Side.Height
	}
	if bottomHeight == 0 {
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
		var uncleBottomHeight uint64
		for e := range sidechain.IterateBlocksInPPLNSWindow(uncle, i.consensus, i.GetDifficultyByHeight, i.GetByTemplateId, nil, func(err error) {
			uncleBottomHeight = 0
		}) {
			uncleBottomHeight = e.Block.Side.Height
		}
		if uncleBottomHeight == 0 {
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

func ChanToSlice[T any](s chan T) (r []T) {
	for v := range s {
		r = append(r, v)
	}
	return r
}
