package api

import (
	"encoding/json"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type P2PoolApi struct {
	Host            string
	Client          *http.Client
	consensus       atomic.Pointer[sidechain.Consensus]
	derivationCache sidechain.DerivationCacheInterface
}

func NewP2PoolApi(host string) *P2PoolApi {
	return &P2PoolApi{
		Host: host,
		Client: &http.Client{
			Timeout: time.Second * 15,
		},
		derivationCache: sidechain.NewDerivationCache(),
	}
}

func (p *P2PoolApi) WaitSync() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panicked")
		}
	}()
	status := p.Status()
	for ; p == nil || !p.Status().Synchronized; status = p.Status() {
		if p == nil {
			log.Printf("[API] Not synchronized (nil), waiting five seconds")
		} else {
			log.Printf("[API] Not synchronized (height %d, id %s), waiting five seconds", status.Height, status.Id)
		}
		time.Sleep(time.Second * 5)
	}
	log.Printf("[API] SYNCHRONIZED (height %d, id %s)", status.Height, status.Id)
	log.Printf("[API] Consensus id = %s\n", p.Consensus().Id())
	return nil
}

func (p *P2PoolApi) ByTemplateId(id types.Hash) *sidechain.PoolBlock {
	if response, err := p.Client.Get(p.Host + "/sidechain/block_by_template_id/" + id.String()); err != nil {
		return nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil
		} else {
			var result p2pooltypes.P2PoolBinaryBlockResult

			if err = json.Unmarshal(buf, &result); err != nil {
				return nil
			} else if result.Version == 0 {
				// Fallback into archive
				if response, err := p.Client.Get(p.Host + "/archive/blocks_by_template_id/" + id.String()); err != nil {
					return nil
				} else {
					defer response.Body.Close()

					if buf, err := io.ReadAll(response.Body); err != nil {
						return nil
					} else {
						var result []p2pooltypes.P2PoolBinaryBlockResult

						if err = json.Unmarshal(buf, &result); err != nil || len(result) == 0 {
							return nil
						}

						for _, r := range result {
							//Get first block that matches
							if r.Version == 0 {
								continue
							}
							b := &sidechain.PoolBlock{
								NetworkType: p.Consensus().NetworkType,
							}
							if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil || int(b.ShareVersion()) != r.Version {
								continue
							}
							return b
						}
						return nil
					}
				}
			}

			b := &sidechain.PoolBlock{
				NetworkType: p.Consensus().NetworkType,
			}
			if err = b.UnmarshalBinary(p.derivationCache, result.Blob); err != nil {
				return nil
			}
			return b
		}
	}
}

func (p *P2PoolApi) BySideHeight(height uint64) []*sidechain.PoolBlock {
	if response, err := p.Client.Get(p.Host + "/sidechain/blocks_by_height/" + strconv.FormatUint(height, 10)); err != nil {
		return nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil
		} else {
			var result []p2pooltypes.P2PoolBinaryBlockResult

			if err = json.Unmarshal(buf, &result); err != nil {
				return nil
			} else if len(result) == 0 {
				// Fallback into archive
				if response, err := p.Client.Get(p.Host + "/archive/blocks_by_side_height/" + strconv.FormatUint(height, 10)); err != nil {
					return nil
				} else {
					defer response.Body.Close()

					if buf, err := io.ReadAll(response.Body); err != nil {
						return nil
					} else {
						var result []p2pooltypes.P2PoolBinaryBlockResult

						if err = json.Unmarshal(buf, &result); err != nil || len(result) == 0 {
							return nil
						}

						results := make([]*sidechain.PoolBlock, 0, len(result))
						for _, r := range result {
							if r.Version == 0 {
								return nil
							}
							b := &sidechain.PoolBlock{
								NetworkType: p.Consensus().NetworkType,
							}
							if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil {
								return nil
							}
							results = append(results, b)
						}
						return results
					}
				}
			}

			results := make([]*sidechain.PoolBlock, 0, len(result))
			for _, r := range result {
				if r.Version == 0 {
					return nil
				}
				b := &sidechain.PoolBlock{
					NetworkType: p.Consensus().NetworkType,
				}
				if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil {
					return nil
				}
				results = append(results, b)
			}
			return results
		}
	}
}

func (p *P2PoolApi) MainHeaderByHeight(height uint64) *block.Header {
	if response, err := p.Client.Get(p.Host + "/mainchain/header_by_height/" + strconv.FormatUint(height, 10)); err != nil {
		return nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil
		} else {
			var result block.Header

			if err = json.Unmarshal(buf, &result); err != nil {
				return nil
			}

			return &result
		}
	}
}

func (p *P2PoolApi) MainDifficultyByHeight(height uint64) types.Difficulty {
	if response, err := p.Client.Get(p.Host + "/mainchain/difficulty_by_height/" + strconv.FormatUint(height, 10)); err != nil {
		return types.ZeroDifficulty
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return types.ZeroDifficulty
		} else {
			var result types.Difficulty

			if err = json.Unmarshal(buf, &result); err != nil {
				return types.ZeroDifficulty
			}

			return result
		}
	}
}

func (p *P2PoolApi) StateFromTemplateId(id types.Hash) (chain, uncles sidechain.UniquePoolBlockSlice) {
	if response, err := p.Client.Get(p.Host + "/sidechain/state/" + id.String()); err != nil {
		return nil, nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil, nil
		} else {
			var result p2pooltypes.P2PoolSideChainStateResult

			if err = json.Unmarshal(buf, &result); err != nil {
				return nil, nil
			}

			chain = make([]*sidechain.PoolBlock, 0, len(result.Chain))
			uncles = make([]*sidechain.PoolBlock, 0, len(result.Uncles))

			for _, r := range result.Chain {
				b := &sidechain.PoolBlock{
					NetworkType: p.Consensus().NetworkType,
				}
				if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil {
					return nil, nil
				}
				chain = append(chain, b)
			}

			for _, r := range result.Uncles {
				b := &sidechain.PoolBlock{
					NetworkType: p.Consensus().NetworkType,
				}
				if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil {
					return nil, nil
				}
				uncles = append(uncles, b)
			}

			return chain, uncles
		}
	}
}

func (p *P2PoolApi) StateFromTip() (chain, uncles sidechain.UniquePoolBlockSlice) {
	if response, err := p.Client.Get(p.Host + "/sidechain/state/tip"); err != nil {
		return nil, nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil, nil
		} else {
			var result p2pooltypes.P2PoolSideChainStateResult

			if err = json.Unmarshal(buf, &result); err != nil {
				return nil, nil
			}

			chain = make([]*sidechain.PoolBlock, 0, len(result.Chain))
			uncles = make([]*sidechain.PoolBlock, 0, len(result.Uncles))

			for _, r := range result.Chain {
				b := &sidechain.PoolBlock{
					NetworkType: p.Consensus().NetworkType,
				}
				if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil {
					return nil, nil
				}
				chain = append(chain, b)
			}

			for _, r := range result.Uncles {
				b := &sidechain.PoolBlock{
					NetworkType: p.Consensus().NetworkType,
				}
				if err = b.UnmarshalBinary(p.derivationCache, r.Blob); err != nil {
					return nil, nil
				}
				uncles = append(uncles, b)
			}

			return chain, uncles
		}
	}
}

func (p *P2PoolApi) Tip() *sidechain.PoolBlock {
	if response, err := p.Client.Get(p.Host + "/sidechain/tip"); err != nil {
		return nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil
		} else {
			var result p2pooltypes.P2PoolBinaryBlockResult

			if err = json.Unmarshal(buf, &result); err != nil {
				return nil
			}

			if result.Version == 0 {
				return nil
			}
			b := &sidechain.PoolBlock{
				NetworkType: p.Consensus().NetworkType,
			}
			if err = b.UnmarshalBinary(p.derivationCache, result.Blob); err != nil {
				return nil
			}
			return b
		}
	}
}

func (p *P2PoolApi) getConsensus() *sidechain.Consensus {
	if response, err := p.Client.Get(p.Host + "/sidechain/consensus"); err != nil {
		return nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil
		} else {
			c, _ := sidechain.NewConsensusFromJSON(buf)

			return c
		}
	}
}

func (p *P2PoolApi) Consensus() *sidechain.Consensus {
	if c := p.consensus.Load(); c == nil {
		if c = p.getConsensus(); c != nil {
			p.consensus.Store(c)
		}
		return c
	} else {
		return c
	}
}

func (p *P2PoolApi) Status() *p2pooltypes.P2PoolSideChainStatusResult {
	if response, err := p.Client.Get(p.Host + "/sidechain/status"); err != nil {
		return nil
	} else {
		defer response.Body.Close()

		if buf, err := io.ReadAll(response.Body); err != nil {
			return nil
		} else {
			result := &p2pooltypes.P2PoolSideChainStatusResult{}

			if err = json.Unmarshal(buf, result); err != nil {
				return nil
			}

			return result
		}
	}
}
