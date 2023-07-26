package index

type IterateFunction[K, V any] func(key K, value V) (stop bool)

type IteratorFunction[K, V any] func(f IterateFunction[K, V]) (complete bool)

type Iterator[K, V any] interface {
	All(f IterateFunction[K, V]) (complete bool)
}

func IteratorToIterator[K, V any](i Iterator[K, V], _ ...error) Iterator[K, V] {
	return i
}

func IterateToSlice[T any](i Iterator[int, T], _ ...error) (s []T) {
	i.All(func(key int, value T) (stop bool) {
		s = append(s, value)
		return false
	})
	return s
}

func IterateToSliceWithoutPointer[T any](i Iterator[int, *T], _ ...error) (s []T) {
	i.All(func(key int, value *T) (stop bool) {
		s = append(s, *value)
		return false
	})
	return s
}

func SliceIterate[S ~[]T, T any](s S, f IterateFunction[int, T]) (complete bool) {
	for i, v := range s {
		if f(i, v) {
			return (len(s) - 1) == i
		}
	}
	return true
}

func ChanIterate[T any](c <-chan T, f IterateFunction[int, T]) (complete bool) {
	var i int
	for {
		v, more := <-c
		if !more {
			return true
		}
		if f(i, v) {
			return false
		}
		i++
	}
}

func ChanConsume[T any](c <-chan T) {
	for range c {

	}
}

func ChanToSlice[S ~[]T, T any](c <-chan T) (s S) {
	s = make(S, 0, len(c))
	for v := range c {
		s = append(s, v)
	}
	return s
}
