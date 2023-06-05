package stratum

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"sync"
	"sync/atomic"
	"time"
)

type MinerTrackingEntry struct {
	Lock         sync.RWMutex
	Counter      atomic.Uint64
	LastTemplate atomic.Uint64
	Templates    map[uint64]*Template
	LastJob      time.Time
}

const JobIdentifierSize = 8 + 4 + 4 + 4 + types.HashSize

type JobIdentifier [JobIdentifierSize]byte

func JobIdentifierFromString(s string) (JobIdentifier, error) {
	var h JobIdentifier
	if buf, err := hex.DecodeString(s); err != nil {
		return h, err
	} else {
		if len(buf) != JobIdentifierSize {
			return h, errors.New("wrong job id size")
		}
		copy(h[:], buf)
		return h, nil
	}
}
func JobIdentifierFromValues(templateCounter uint64, extraNonce, sideRandomNumber, sideExtraNonce uint32, templateId types.Hash) JobIdentifier {
	var h JobIdentifier
	binary.LittleEndian.PutUint64(h[:], templateCounter)
	binary.LittleEndian.PutUint32(h[8:], extraNonce)
	binary.LittleEndian.PutUint32(h[8+4:], sideRandomNumber)
	binary.LittleEndian.PutUint32(h[8+4+4:], sideExtraNonce)
	copy(h[8+4+4+4:], templateId[:])
	return h
}

func (id JobIdentifier) TemplateCounter() uint64 {
	return binary.LittleEndian.Uint64(id[:])
}

func (id JobIdentifier) ExtraNonce() uint32 {
	return binary.LittleEndian.Uint32(id[8:])
}

func (id JobIdentifier) SideRandomNumber() uint32 {
	return binary.LittleEndian.Uint32(id[8+4:])
}

func (id JobIdentifier) SideExtraNonce() uint32 {
	return binary.LittleEndian.Uint32(id[8+4+4:])
}

func (id JobIdentifier) TemplateId() types.Hash {
	return types.HashFromBytes(id[8+4+4+4 : 8+4+4+4+types.HashSize])
}

func (id JobIdentifier) String() string {
	return hex.EncodeToString(id[:])
}

// GetJobBlob Gets old job data based on returned id
func (e *MinerTrackingEntry) GetJobBlob(jobId JobIdentifier, nonce uint32) []byte {
	e.Lock.RLock()
	defer e.Lock.RUnlock()

	if t, ok := e.Templates[jobId.TemplateCounter()]; ok {
		buffer := bytes.NewBuffer(make([]byte, 0, len(t.Buffer)))
		if err := t.Write(buffer, nonce, jobId.ExtraNonce(), jobId.SideRandomNumber(), jobId.SideExtraNonce(), jobId.TemplateId()); err != nil {
			return nil
		}
		return buffer.Bytes()
	} else {
		return nil
	}
}
