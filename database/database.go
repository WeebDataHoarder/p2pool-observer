package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	_ "github.com/lib/pq"
	"log"
	"reflect"
	"sync"
)

type Database struct {
	handle     *sql.DB
	statements struct {
		GetMinerById            *sql.Stmt
		GetMinerByAddress       *sql.Stmt
		GetMinerByAddressBounds *sql.Stmt
		InsertMiner             *sql.Stmt
	}

	cacheLock           sync.RWMutex
	minerCache          map[uint64]*Miner
	unclesByParentCache map[types.Hash][]*UncleBlock
}

func NewDatabase(connStr string) (db *Database, err error) {
	db = &Database{
		minerCache:          make(map[uint64]*Miner, 1024),
		unclesByParentCache: make(map[types.Hash][]*UncleBlock, p2pool.PPLNSWindow*16),
	}
	if db.handle, err = sql.Open("postgres", connStr); err != nil {
		return nil, err
	}

	if db.statements.GetMinerById, err = db.handle.Prepare("SELECT id, address FROM miners WHERE id = $1;"); err != nil {
		return nil, err
	}
	if db.statements.GetMinerByAddress, err = db.handle.Prepare("SELECT id, address FROM miners WHERE address = $1;"); err != nil {
		return nil, err
	}
	if db.statements.GetMinerByAddressBounds, err = db.handle.Prepare("SELECT id, address FROM miners WHERE address LIKE $1 AND address LIKE $2;"); err != nil {
		return nil, err
	}
	if db.statements.InsertMiner, err = db.handle.Prepare("INSERT INTO miners (address) VALUES ($1) RETURNING id, address;"); err != nil {
		return nil, err
	}
	if err != nil {
		log.Fatal(err)
	}

	return db, nil
}

//cache methods

func (db *Database) GetMiner(miner uint64) *Miner {
	if m := func() *Miner {
		db.cacheLock.RLock()
		defer db.cacheLock.RUnlock()
		return db.minerCache[miner]
	}(); m != nil {
		return m
	} else if m = db.getMiner(miner); m != nil {
		db.cacheLock.Lock()
		defer db.cacheLock.Unlock()
		db.minerCache[miner] = m
		return m
	} else {
		return nil
	}
}

func (db *Database) GetUnclesByParentId(id types.Hash) chan *UncleBlock {
	if c := func() chan *UncleBlock {
		db.cacheLock.RLock()
		defer db.cacheLock.RUnlock()
		if s, ok := db.unclesByParentCache[id]; ok {
			c := make(chan *UncleBlock, len(s))
			defer close(c)
			for _, u := range s {
				c <- u
			}
			return c
		}
		return nil
	}(); c != nil {
		return c
	} else if c = db.getUnclesByParentId(id); c != nil {
		c2 := make(chan *UncleBlock)
		go func() {
			val := make([]*UncleBlock, 0, 6)
			defer func() {
				db.cacheLock.Lock()
				defer db.cacheLock.Unlock()
				if len(db.unclesByParentCache) >= p2pool.PPLNSWindow*16 {
					for k := range db.unclesByParentCache {
						delete(db.unclesByParentCache, k)
						break
					}
				}
				db.unclesByParentCache[id] = val
			}()
			defer close(c2)
			for u := range c {
				val = append(val, u)
				c2 <- u
			}
		}()
		return c2
	} else {
		return c
	}
}

func (db *Database) getMiner(miner uint64) *Miner {
	if rows, err := db.statements.GetMinerById.Query(miner); err != nil {
		return nil
	} else {
		defer rows.Close()
		if rows.Next() {
			m := &Miner{}
			if err = rows.Scan(&m.id, &m.addr); err != nil {
				return nil
			}
			return m
		}

		return nil
	}
}

func (db *Database) GetMinerByAddress(addr string) *Miner {
	if rows, err := db.statements.GetMinerByAddress.Query(addr); err != nil {
		return nil
	} else {
		defer rows.Close()
		if rows.Next() {
			m := &Miner{}
			if err = rows.Scan(&m.id, &m.addr); err != nil {
				return nil
			}
			return m
		}

		return nil
	}
}

func (db *Database) GetOrCreateMinerByAddress(addr string) *Miner {
	if m := db.GetMinerByAddress(addr); m != nil {
		return m
	} else {
		if rows, err := db.statements.InsertMiner.Query(addr); err != nil {
			return nil
		} else {
			defer rows.Close()
			if rows.Next() {
				m = &Miner{}
				if err = rows.Scan(&m.id, &m.addr); err != nil {
					return nil
				}
				return m
			}

			return nil
		}
	}
}

func (db *Database) GetMinerByAddressBounds(addrStart, addrEnd string) *Miner {
	if rows, err := db.statements.GetMinerByAddress.Query(addrStart+"%", "%"+addrEnd); err != nil {
		return nil
	} else {
		defer rows.Close()
		if rows.Next() {
			m := &Miner{}
			if err = rows.Scan(&m.id, &m.addr); err != nil {
				return nil
			}
			return m
		}

		return nil
	}
}

type RowScanInterface interface {
	Scan(dest ...any) error
}

func (db *Database) Query(query string, callback func(row RowScanInterface) error, params ...any) error {
	if stmt, err := db.handle.Prepare(query); err != nil {
		return err
	} else {
		defer stmt.Close()

		return db.QueryStatement(stmt, callback, params...)
	}
}

func (db *Database) QueryStatement(stmt *sql.Stmt, callback func(row RowScanInterface) error, params ...any) error {
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

func (db *Database) GetBlocksByQuery(where string, params ...any) chan *Block {
	returnChannel := make(chan *Block)
	go func() {
		defer close(returnChannel)
		err := db.Query(fmt.Sprintf("SELECT decode(id, 'hex'), height, decode(previous_id, 'hex'), decode(coinbase_id, 'hex'), coinbase_reward, decode(coinbase_privkey, 'hex'), difficulty, timestamp, miner, decode(pow_hash, 'hex'), main_height, decode(main_id, 'hex'), main_found, decode(miner_main_id, 'hex'), miner_main_difficulty FROM blocks %s;", where), func(row RowScanInterface) (err error) {
			block := &Block{}

			var difficultyHex, minerMainDifficultyHex string

			var (
				IdPtr                 = block.Id[:]
				PreviousIdPtr         = block.PreviousId[:]
				CoinbaseIdPtr         = block.Coinbase.Id[:]
				CoinbasePrivateKeyPtr = block.Coinbase.PrivateKey[:]
				PowHashPtr            = block.PowHash[:]
				MainIdPtr             = block.Main.Id[:]
				MinerMainId           = block.Template.Id[:]
			)

			if err = row.Scan(&IdPtr, &block.Height, &PreviousIdPtr, &CoinbaseIdPtr, &block.Coinbase.Reward, &CoinbasePrivateKeyPtr, &difficultyHex, &block.Timestamp, &block.MinerId, &PowHashPtr, &block.Main.Height, &MainIdPtr, &block.Main.Found, &MinerMainId, &minerMainDifficultyHex); err != nil {
				return err
			}

			copy(block.Id[:], IdPtr)
			copy(block.PreviousId[:], PreviousIdPtr)
			copy(block.Coinbase.Id[:], CoinbaseIdPtr)
			copy(block.Coinbase.PrivateKey[:], CoinbasePrivateKeyPtr)
			copy(block.PowHash[:], PowHashPtr)
			copy(block.Main.Id[:], MainIdPtr)
			copy(block.Template.Id[:], MinerMainId)

			block.Difficulty, _ = types.DifficultyFromString(difficultyHex)
			block.Template.Difficulty, _ = types.DifficultyFromString(minerMainDifficultyHex)

			returnChannel <- block

			return nil
		}, params...)
		if err != nil {
			log.Print(err)
		}
	}()

	return returnChannel
}

func (db *Database) GetUncleBlocksByQuery(where string, params ...any) chan *UncleBlock {
	returnChannel := make(chan *UncleBlock)
	go func() {
		defer close(returnChannel)
		err := db.Query(fmt.Sprintf("SELECT decode(parent_id, 'hex'), parent_height, decode(id, 'hex'), height, decode(previous_id, 'hex'), decode(coinbase_id, 'hex'), coinbase_reward, decode(coinbase_privkey, 'hex'), difficulty, timestamp, miner, decode(pow_hash, 'hex'), main_height, decode(main_id, 'hex'), main_found, decode(miner_main_id, 'hex'), miner_main_difficulty FROM uncles %s;", where), func(row RowScanInterface) (err error) {
			uncle := &UncleBlock{}
			block := &uncle.Block

			var difficultyHex, minerMainDifficultyHex string

			var (
				ParentId              = uncle.ParentId[:]
				IdPtr                 = block.Id[:]
				PreviousIdPtr         = block.PreviousId[:]
				CoinbaseIdPtr         = block.Coinbase.Id[:]
				CoinbasePrivateKeyPtr = block.Coinbase.PrivateKey[:]
				PowHashPtr            = block.PowHash[:]
				MainIdPtr             = block.Main.Id[:]
				MinerMainId           = block.Template.Id[:]
			)

			if err = row.Scan(&ParentId, &uncle.ParentHeight, &IdPtr, &block.Height, &PreviousIdPtr, &CoinbaseIdPtr, &block.Coinbase.Reward, &CoinbasePrivateKeyPtr, &difficultyHex, &block.Timestamp, &block.MinerId, &PowHashPtr, &block.Main.Height, &MainIdPtr, &block.Main.Found, &MinerMainId, &minerMainDifficultyHex); err != nil {
				return err
			}

			copy(uncle.ParentId[:], ParentId)
			copy(block.Id[:], IdPtr)
			copy(block.PreviousId[:], PreviousIdPtr)
			copy(block.Coinbase.Id[:], CoinbaseIdPtr)
			copy(block.Coinbase.PrivateKey[:], CoinbasePrivateKeyPtr)
			copy(block.PowHash[:], PowHashPtr)
			copy(block.Main.Id[:], MainIdPtr)
			copy(block.Template.Id[:], MinerMainId)

			block.Difficulty, _ = types.DifficultyFromString(difficultyHex)
			block.Template.Difficulty, _ = types.DifficultyFromString(minerMainDifficultyHex)

			returnChannel <- uncle

			return nil
		}, params...)
		if err != nil {
			log.Print(err)
		}
	}()

	return returnChannel
}

func (db *Database) GetBlockById(id types.Hash) *Block {
	r := db.GetBlocksByQuery("WHERE id = $1;", id.String())
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (db *Database) GetBlockByPreviousId(id types.Hash) *Block {
	r := db.GetBlocksByQuery("WHERE previous_id = $1;", id.String())
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (db *Database) GetBlockByHeight(height uint64) *Block {
	r := db.GetBlocksByQuery("WHERE height = $1;", height)
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (db *Database) GetBlocksInWindow(startHeight *uint64, windowSize uint64) chan *Block {
	if windowSize == 0 {
		windowSize = p2pool.PPLNSWindow
	}

	if startHeight == nil {
		return db.GetBlocksByQuery("WHERE height > ((SELECT MAX(height) FROM blocks) - $1) AND height <= ((SELECT MAX(height) FROM blocks)) ORDER BY height DESC", windowSize)
	} else {
		return db.GetBlocksByQuery("WHERE height > ($1) AND height <= ($2) ORDER BY height DESC", *startHeight-windowSize, *startHeight)
	}
}

func (db *Database) GetBlocksByMinerIdInWindow(minerId uint64, startHeight *uint64, windowSize uint64) chan *Block {
	if windowSize == 0 {
		windowSize = p2pool.PPLNSWindow
	}

	if startHeight == nil {
		return db.GetBlocksByQuery("WHERE height > ((SELECT MAX(height) FROM blocks) - $2) AND height <= ((SELECT MAX(height) FROM blocks)) AND miner = $1 ORDER BY height DESC", minerId, windowSize)
	} else {
		return db.GetBlocksByQuery("WHERE height > ($2) AND height <= ($3) AND miner = $1 ORDER BY height DESC", minerId, *startHeight-windowSize, *startHeight)
	}
}

func (db *Database) GetChainTip() *Block {
	r := db.GetBlocksByQuery("ORDER BY height DESC LIMIT 1;")
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (db *Database) GetLastFound() BlockInterface {
	r := db.GetAllFound(1)
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (db *Database) GetUncleById(id types.Hash) *UncleBlock {
	r := db.GetUncleBlocksByQuery("WHERE id = $1;", id.String())
	defer func() {
		for range r {

		}
	}()
	return <-r
}

func (db *Database) getUnclesByParentId(id types.Hash) chan *UncleBlock {
	return db.GetUncleBlocksByQuery("WHERE parent_id = $1;", id.String())
}

func (db *Database) GetUnclesByParentHeight(height uint64) chan *UncleBlock {
	return db.GetUncleBlocksByQuery("WHERE parent_height = $1;", height)
}

func (db *Database) GetUnclesInWindow(startHeight *uint64, windowSize uint64) chan *UncleBlock {
	if windowSize == 0 {
		windowSize = p2pool.PPLNSWindow
	}

	//TODO add p2pool.UncleBlockDepth ?
	if startHeight == nil {
		return db.GetUncleBlocksByQuery("WHERE parent_height > ((SELECT MAX(height) FROM blocks) - $1) AND parent_height <= ((SELECT MAX(height) FROM blocks)) AND height > ((SELECT MAX(height) FROM blocks) - $1) AND height <= ((SELECT MAX(height) FROM blocks)) ORDER BY height DESC", windowSize)
	} else {
		return db.GetUncleBlocksByQuery("WHERE parent_height > ($1) AND parent_height <= ($2) AND height > ($1) AND height <= ($2)  ORDER BY height DESC", *startHeight-windowSize, *startHeight)
	}
}

func (db *Database) GetUnclesByMinerIdInWindow(minerId uint64, startHeight *uint64, windowSize uint64) chan *UncleBlock {
	if windowSize == 0 {
		windowSize = p2pool.PPLNSWindow
	}

	//TODO add p2pool.UncleBlockDepth ?
	if startHeight == nil {
		return db.GetUncleBlocksByQuery("WHERE parent_height > ((SELECT MAX(height) FROM blocks) - $2) AND parent_height <= ((SELECT MAX(height) FROM blocks)) AND height > ((SELECT MAX(height) FROM blocks) - $2) AND height <= ((SELECT MAX(height) FROM blocks)) AND miner = $1 ORDER BY height DESC", minerId, windowSize)
	} else {
		return db.GetUncleBlocksByQuery("WHERE parent_height > ($2) AND parent_height <= ($3) AND height > ($2) AND height <= ($3) AND miner = $1 ORDER BY height DESC", minerId, *startHeight-windowSize, *startHeight)
	}
}

func (db *Database) GetAllFound(limit uint64) chan BlockInterface {
	blocks := db.GetFound(limit)
	uncles := db.GetFoundUncles(limit)

	result := make(chan BlockInterface)
	go func() {
		defer close(result)
		defer func() {
			for range blocks {

			}
		}()
		defer func() {
			for range uncles {

			}
		}()

		var i uint64

		var currentBlock *Block
		var currentUncle *UncleBlock

		for {
			var current BlockInterface

			if limit != 0 && i >= limit {
				break
			}

			if currentBlock == nil {
				currentBlock = <-blocks
			}
			if currentUncle == nil {
				currentUncle = <-uncles
			}

			if currentBlock != nil {
				if current == nil || currentBlock.Main.Height > current.GetBlock().Main.Height {
					current = currentBlock
				}
			}

			if currentUncle != nil {
				if current == nil || currentUncle.Block.Main.Height > current.GetBlock().Main.Height {
					current = currentUncle
				}
			}

			if current == nil {
				break
			}

			if currentBlock == current {
				currentBlock = nil
			} else if currentUncle == current {
				currentUncle = nil
			}

			result <- current

			i++
		}
	}()
	return result
}

func (db *Database) GetShares(limit uint64, minerId uint64, onlyBlocks bool) chan BlockInterface {
	var blocks chan *Block
	var uncles chan *UncleBlock

	if limit == 0 {
		if minerId != 0 {
			blocks = db.GetBlocksByQuery("WHERE miner = $1 ORDER BY height DESC;", minerId)
			if !onlyBlocks {
				uncles = db.GetUncleBlocksByQuery("WHERE miner = $1 ORDER BY height DESC, timestamp DESC;", minerId)
			}
		} else {
			blocks = db.GetBlocksByQuery("ORDER BY height DESC;")
			if !onlyBlocks {
				uncles = db.GetUncleBlocksByQuery("ORDER BY height DESC, timestamp DESC;")
			}
		}
	} else {
		if minerId != 0 {
			blocks = db.GetBlocksByQuery("WHERE miner = $2 ORDER BY height DESC LIMIT $1;", limit, minerId)
			if !onlyBlocks {
				uncles = db.GetUncleBlocksByQuery("WHERE miner = $2 ORDER BY height DESC, timestamp DESC LIMIT $1;", limit, minerId)
			}
		} else {
			blocks = db.GetBlocksByQuery("ORDER BY height DESC LIMIT $1;", limit)
			if !onlyBlocks {
				uncles = db.GetUncleBlocksByQuery("ORDER BY height DESC, timestamp DESC LIMIT $1;", limit)
			}
		}
	}

	result := make(chan BlockInterface)
	go func() {
		defer func() {
			if blocks == nil {
				return
			}
			for range blocks {

			}
		}()
		defer func() {
			if uncles == nil {
				return
			}
			for range uncles {

			}
		}()
		defer close(result)

		var i uint64

		var currentBlock *Block
		var currentUncle *UncleBlock

		for {
			var current BlockInterface

			if limit != 0 && i >= limit {
				break
			}

			if currentBlock == nil {
				currentBlock = <-blocks
			}
			if !onlyBlocks && currentUncle == nil {
				currentUncle = <-uncles
			}

			if currentBlock != nil {
				if current == nil || currentBlock.Height > current.GetBlock().Height {
					current = currentBlock
				}
			}

			if !onlyBlocks && currentUncle != nil {
				if current == nil || currentUncle.Block.Height > current.GetBlock().Height {
					current = currentUncle
				}
			}

			if current == nil {
				break
			}

			if currentBlock == current {
				currentBlock = nil
			} else if !onlyBlocks && currentUncle == current {
				currentUncle = nil
			}

			result <- current

			i++
		}
	}()
	return result
}

func (db *Database) GetFound(limit uint64) chan *Block {
	if limit == 0 {
		return db.GetBlocksByQuery("WHERE main_found IS TRUE ORDER BY main_height DESC;")
	} else {
		return db.GetBlocksByQuery("WHERE main_found IS TRUE ORDER BY main_height DESC LIMIT $1;", limit)
	}
}

func (db *Database) GetFoundUncles(limit uint64) chan *UncleBlock {
	if limit == 0 {
		return db.GetUncleBlocksByQuery("WHERE main_found IS TRUE ORDER BY main_height DESC;")
	} else {
		return db.GetUncleBlocksByQuery("WHERE main_found IS TRUE ORDER BY main_height DESC LIMIT $1;", limit)
	}
}

func (db *Database) DeleteBlockById(id types.Hash) (n int, err error) {
	if block := db.GetBlockById(id); block == nil {
		return 0, nil
	} else {
		for {
			if err = db.Query("DELETE FROM coinbase_outputs WHERE id = (SELECT coinbase_id FROM blocks WHERE id = $1) OR id = (SELECT coinbase_id FROM uncles WHERE id = $1);", nil, id.String()); err != nil {
				return n, err
			} else if err = db.Query("DELETE FROM uncles WHERE parent_id = $1;", nil, id.String()); err != nil {
				return n, err
			} else if err = db.Query("DELETE FROM blocks WHERE id = $1;", nil, id.String()); err != nil {
				return n, err
			}
			block = db.GetBlockByPreviousId(block.Id)
			if block == nil {
				return n, nil
			}
			n++
		}
	}
}

func (db *Database) SetBlockMainDifficulty(id types.Hash, difficulty types.Difficulty) error {
	if err := db.Query("UPDATE blocks SET miner_main_difficulty = $2 WHERE id = $1;", nil, id.String(), difficulty.String()); err != nil {
		return err
	} else if err = db.Query("UPDATE uncles SET miner_main_difficulty = $2 WHERE id = $1;", nil, id.String(), difficulty.String()); err != nil {
		return err
	}

	return nil
}

func (db *Database) SetBlockFound(id types.Hash, found bool) error {
	if err := db.Query("UPDATE blocks SET main_found = $2 WHERE id = $1;", nil, id.String(), found); err != nil {
		return err
	} else if err = db.Query("UPDATE uncles SET main_found = $2 WHERE id = $1;", nil, id.String(), found); err != nil {
		return err
	}

	if !found {
		if err := db.Query("DELETE FROM coinbase_outputs WHERE id = (SELECT coinbase_id FROM blocks WHERE id = $1) OR id = (SELECT coinbase_id FROM uncles WHERE id = $1);", nil, id.String()); err != nil {
			return err
		}
	}

	return nil
}

func (db *Database) CoinbaseTransactionExists(block *Block) bool {
	var count uint64
	if err := db.Query("SELECT COUNT(*) as count FROM coinbase_outputs WHERE id = $1;", func(row RowScanInterface) error {
		return row.Scan(&count)
	}, block.Coinbase.Id.String()); err != nil {
		return false
	}

	return count > 0
}

func (db *Database) GetCoinbaseTransaction(block *Block) *CoinbaseTransaction {
	var outputs []*CoinbaseTransactionOutput
	if err := db.Query("SELECT index, amount, miner FROM coinbase_outputs WHERE id = $1 ORDER BY index DESC;", func(row RowScanInterface) error {
		output := &CoinbaseTransactionOutput{
			id: block.Coinbase.Id,
		}

		if err := row.Scan(&output.index, &output.amount, &output.miner); err != nil {
			return err
		}
		outputs = append(outputs, output)
		return nil
	}, block.Coinbase.Id.String()); err != nil {
		return nil
	}

	return &CoinbaseTransaction{
		id:         block.Coinbase.Id,
		privateKey: block.Coinbase.PrivateKey,
		outputs:    outputs,
	}
}

func (db *Database) GetCoinbaseTransactionOutputByIndex(coinbaseId types.Hash, index uint64) *CoinbaseTransactionOutput {
	output := &CoinbaseTransactionOutput{
		id:    coinbaseId,
		index: index,
	}
	if err := db.Query("SELECT amount, miner FROM coinbase_outputs WHERE id = $1 AND index = $2 ORDER BY index DESC;", func(row RowScanInterface) error {
		if err := row.Scan(&output.amount, &output.miner); err != nil {
			return err
		}
		return nil
	}, coinbaseId.String(), index); err != nil {
		return nil
	}

	return output
}

func (db *Database) GetCoinbaseTransactionOutputByMinerId(coinbaseId types.Hash, minerId uint64) *CoinbaseTransactionOutput {
	output := &CoinbaseTransactionOutput{
		id:    coinbaseId,
		miner: minerId,
	}
	if err := db.Query("SELECT amount, index FROM coinbase_outputs WHERE id = $1 AND miner = $2 ORDER BY index DESC;", func(row RowScanInterface) error {
		if err := row.Scan(&output.amount, &output.index); err != nil {
			return err
		}
		return nil
	}, coinbaseId.String(), minerId); err != nil {
		return nil
	}

	return output
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
		Id         types.Hash `json:"id"`
		Reward     uint64     `json:"reward"`
		PrivateKey types.Hash `json:"private_key"`
		Index      uint64     `json:"index"`
	} `json:"coinbase"`
}

func (db *Database) GetPayoutsByMinerId(minerId uint64, limit uint64) chan *Payout {
	out := make(chan *Payout)

	go func() {
		defer close(out)

		miner := db.getMiner(minerId)

		if miner == nil {
			return
		}

		resultFunc := func(row RowScanInterface) error {
			var blockId, mainId, privKey, coinbaseId []byte
			var height, mainHeight, timestamp, amount, index uint64
			var uncle bool

			if err := row.Scan(&blockId, &mainId, &height, &mainHeight, &timestamp, &privKey, &uncle, &coinbaseId, &amount, &index); err != nil {
				return err
			}

			out <- &Payout{
				Id:        types.HashFromBytes(blockId),
				Height:    height,
				Timestamp: timestamp,
				Main: struct {
					Id     types.Hash `json:"id"`
					Height uint64     `json:"height"`
				}{Id: types.HashFromBytes(mainId), Height: mainHeight},
				Uncle: uncle,
				Coinbase: struct {
					Id         types.Hash `json:"id"`
					Reward     uint64     `json:"reward"`
					PrivateKey types.Hash `json:"private_key"`
					Index      uint64     `json:"index"`
				}{Id: types.HashFromBytes(coinbaseId), Reward: amount, PrivateKey: types.HashFromBytes(privKey), Index: index},
			}
			return nil
		}

		if limit == 0 {
			if err := db.Query("SELECT decode(b.id, 'hex') AS id, decode(b.main_id, 'hex') AS main_id, b.height AS height, b.main_height AS main_height, b.timestamp AS timestamp, decode(b.coinbase_privkey, 'hex') AS coinbase_privkey, b.uncle AS uncle, decode(o.id, 'hex') AS coinbase_id, o.amount AS amount, o.index AS index FROM (SELECT id, amount, index FROM coinbase_outputs WHERE miner = $1) o LEFT JOIN LATERAL\n(SELECT id, coinbase_id, coinbase_privkey, height, main_height, main_id, timestamp, FALSE AS uncle FROM blocks WHERE coinbase_id = o.id UNION SELECT id, coinbase_id, coinbase_privkey, height, main_height, main_id, timestamp, TRUE AS uncle FROM uncles WHERE coinbase_id = o.id) b ON b.coinbase_id = o.id ORDER BY main_height DESC;", resultFunc, minerId); err != nil {
				return
			}
		} else {
			if err := db.Query("SELECT decode(b.id, 'hex') AS id, decode(b.main_id, 'hex') AS main_id, b.height AS height, b.main_height AS main_height, b.timestamp AS timestamp, decode(b.coinbase_privkey, 'hex') AS coinbase_privkey, b.uncle AS uncle, decode(o.id, 'hex') AS coinbase_id, o.amount AS amount, o.index AS index FROM (SELECT id, amount, index FROM coinbase_outputs WHERE miner = $1) o LEFT JOIN LATERAL\n(SELECT id, coinbase_id, coinbase_privkey, height, main_height, main_id, timestamp, FALSE AS uncle FROM blocks WHERE coinbase_id = o.id UNION SELECT id, coinbase_id, coinbase_privkey, height, main_height, main_id, timestamp, TRUE AS uncle FROM uncles WHERE coinbase_id = o.id) b ON b.coinbase_id = o.id ORDER BY main_height DESC LIMIT $2;", resultFunc, minerId, limit); err != nil {
				return
			}
		}
	}()

	return out
}

func (db *Database) InsertCoinbaseTransaction(coinbase *CoinbaseTransaction) error {
	if tx, err := db.handle.BeginTx(context.Background(), nil); err != nil {
		return err
	} else if stmt, err := tx.Prepare("INSERT INTO coinbase_outputs (id, index, miner, amount) VALUES ($1, $2, $3, $4);"); err != nil {
		_ = tx.Rollback()
		return err
	} else {
		defer stmt.Close()
		for _, o := range coinbase.Outputs() {
			if rows, err := stmt.Query(o.Id().String(), o.Index(), o.Miner(), o.Amount()); err != nil {
				_ = tx.Rollback()
				return err
			} else {
				if err = rows.Close(); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
		}

		return tx.Commit()
	}
}

func (db *Database) InsertBlock(b *Block, fallbackDifficulty *types.Difficulty) error {

	block := db.GetBlockById(b.Id)
	mainDiff := b.Template.Difficulty
	if mainDiff == UndefinedDifficulty && fallbackDifficulty != nil {
		mainDiff = *fallbackDifficulty
	}

	if block != nil { //Update found status if existent
		if block.Template.Difficulty != mainDiff && mainDiff != UndefinedDifficulty {
			if err := db.SetBlockMainDifficulty(block.Id, mainDiff); err != nil {
				return err
			}
		}

		if b.Main.Found && !block.Main.Found {
			if err := db.SetBlockFound(block.Id, true); err != nil {
				return err
			}
		}

		return nil
	}

	return db.Query(
		"INSERT INTO blocks (id, height, previous_id, coinbase_id, coinbase_reward, coinbase_privkey, difficulty, timestamp, miner, pow_hash, main_height, main_id, main_found, miner_main_id, miner_main_difficulty) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);",
		nil,
		b.Id.String(),
		b.Height,
		b.PreviousId.String(),
		b.Coinbase.Id.String(),
		b.Coinbase.Reward,
		b.Coinbase.PrivateKey.String(),
		b.Difficulty.String(),
		b.Timestamp,
		b.MinerId,
		b.PowHash.String(),
		b.Main.Height,
		b.Main.Id.String(),
		b.Main.Found,
		b.Template.Id.String(),
		b.Template.Difficulty.String(),
	)
}

func (db *Database) InsertUncleBlock(u *UncleBlock, fallbackDifficulty *types.Difficulty) error {

	if b := db.GetBlockById(u.ParentId); b == nil {
		return errors.New("parent does not exist")
	}

	uncle := db.GetUncleById(u.Block.Id)

	mainDiff := u.Block.Template.Difficulty
	if mainDiff == UndefinedDifficulty && fallbackDifficulty != nil {
		mainDiff = *fallbackDifficulty
	}

	if uncle != nil { //Update found status if existent
		if uncle.Block.Template.Difficulty != mainDiff && mainDiff != UndefinedDifficulty {
			if err := db.SetBlockMainDifficulty(u.Block.Id, mainDiff); err != nil {
				return err
			}
		}

		if u.Block.Main.Found && !uncle.Block.Main.Found {
			if err := db.SetBlockFound(u.Block.Id, true); err != nil {
				return err
			}
		}

		return nil
	}

	return db.Query(
		"INSERT INTO uncles (parent_id, parent_height, id, height, previous_id, coinbase_id, coinbase_reward, coinbase_privkey, difficulty, timestamp, miner, pow_hash, main_height, main_id, main_found, miner_main_id, miner_main_difficulty) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17);",
		nil,
		u.ParentId.String(),
		u.ParentHeight,
		u.Block.Id.String(),
		u.Block.Height,
		u.Block.PreviousId.String(),
		u.Block.Coinbase.Id.String(),
		u.Block.Coinbase.Reward,
		u.Block.Coinbase.PrivateKey.String(),
		u.Block.Difficulty.String(),
		u.Block.Timestamp,
		u.Block.MinerId,
		u.Block.PowHash.String(),
		u.Block.Main.Height,
		u.Block.Main.Id.String(),
		u.Block.Main.Found,
		u.Block.Template.Id.String(),
		u.Block.Template.Difficulty.String(),
	)
}

func (db *Database) Close() error {

	//cleanup statements
	v := reflect.ValueOf(db.statements)
	for i := 0; i < v.NumField(); i++ {
		if stmt, ok := v.Field(i).Interface().(*sql.Stmt); ok && stmt != nil {
			//v.Field(i).Elem().Set(reflect.ValueOf((*sql.Stmt)(nil)))
			stmt.Close()
		}
	}

	return db.handle.Close()
}
