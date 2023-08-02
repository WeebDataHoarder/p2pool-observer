package index

import (
	"database/sql"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
)

type RowScanInterface interface {
	Scan(dest ...any) error
}

type Scannable interface {
	ScanFromRow(consensus *sidechain.Consensus, row RowScanInterface) error
}

type QueryIterator[V any] interface {
	All(f IterateFunction[int, *V]) (complete bool)
	Next() (int, *V)
	Close()
	Err() error
}

func QueryIterate[V any](i QueryIterator[V], f IterateFunction[int, *V]) {
	if i == nil {
		return
	}
	defer i.Close()
	i.All(f)

	if i.Err() != nil {
		panic(i.Err())
	}
}

func QueryIterateToSlice[T any](i QueryIterator[T], err ...error) (s []*T) {
	if len(err) > 0 {
		if err[0] != nil {
			panic(err)
		}
	}

	QueryIterate(i, func(key int, value *T) (stop bool) {
		s = append(s, value)
		return false
	})
	return s
}

func QueryFirstResult[T any](i QueryIterator[T], err ...error) (v *T) {
	if len(err) > 0 {
		if err[0] != nil {
			panic(err)
		}
	}

	QueryIterate(i, func(_ int, value *T) (stop bool) {
		v = value
		return true
	})
	return
}

func QueryHasResults[T any](i QueryIterator[T], err ...error) bool {
	if len(err) > 0 {
		if err[0] != nil {
			panic(err)
		}
	}

	var hasValue bool
	QueryIterate(i, func(key int, value *T) (stop bool) {
		if value != nil {
			hasValue = true
		}
		return true
	})
	return hasValue
}

// queryStatement Queries a provided sql.Stmt and returns a QueryIterator
// After results are read QueryIterator must be closed/freed
func queryStatement[V any](index *Index, stmt *sql.Stmt, params ...any) (*QueryResult[V], error) {
	var testV *V
	if _, ok := any(testV).(Scannable); !ok {
		return nil, errors.New("unsupported type")
	}

	if rows, err := stmt.Query(params...); err != nil {
		return nil, err
	} else {
		return &QueryResult[V]{
			consensus: index.consensus,
			rows:      rows,
		}, err
	}
}

type FakeQueryResult[V any] struct {
	NextFunction func() (int, *V)
}

func (r *FakeQueryResult[V]) All(f IterateFunction[int, *V]) (complete bool) {
	for {
		if i, v := r.Next(); v == nil {
			return true
		} else {
			if f(i, v) {
				// do not allow resuming
				return true
			}
		}
	}
}

func (r *FakeQueryResult[V]) Next() (int, *V) {
	return r.NextFunction()
}

func (r *FakeQueryResult[V]) Err() error {
	return nil
}

func (r *FakeQueryResult[V]) Close() {

}

type QueryResult[V any] struct {
	consensus *sidechain.Consensus
	rows      *sql.Rows
	closer    func()
	i         int
	err       error
}

func (r *QueryResult[V]) All(f IterateFunction[int, *V]) (complete bool) {
	for {
		if i, v := r.Next(); v == nil {
			return true
		} else {
			if f(i, v) {
				// do not allow resuming
				return true
			}
		}
	}
}

func (r *QueryResult[V]) Next() (int, *V) {
	if r.rows.Next() {
		var v V
		if r.err = any(&v).(Scannable).ScanFromRow(r.consensus, r.rows); r.err != nil {
			return 0, nil
		}
		r.i++
		return r.i - 1, &v
	}
	return 0, nil
}

func (r *QueryResult[V]) Err() error {
	return r.err
}

func (r *QueryResult[V]) Close() {
	if r.closer != nil {
		r.closer()
		r.closer = nil
	}
	if r.rows != nil {
		r.rows.Close()
		r.rows = nil
	}
}
