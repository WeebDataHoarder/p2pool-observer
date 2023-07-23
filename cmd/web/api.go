package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func getTypeFromAPI[T any](method string, cacheTime ...int) *T {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	return cacheResult[*T](method, time.Second*time.Duration(cTime), func() *T {
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return nil
		} else {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				var result T
				decoder := utils.NewJSONDecoder(response.Body)
				if decoder.Decode(&result) != nil {
					return nil
				} else {
					return &result
				}
			} else {
				return nil
			}
		}
	})
}

func getSliceFromAPI[T any](method string, cacheTime ...int) []T {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	return cacheResult[[]T](method, time.Second*time.Duration(cTime), func() []T {
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return nil
		} else {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				var result []T
				decoder := utils.NewJSONDecoder(response.Body)
				if decoder.Decode(&result) != nil {
					return nil
				} else {
					return result
				}
			} else {
				return nil
			}
		}
	})
}

func getStreamFromAPI[T any](method string) <-chan T {
	result := make(chan T, 1)

	go func() {
		defer close(result)
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return
		} else {
			defer response.Body.Close()
			defer io.ReadAll(response.Body)

			if response.StatusCode == http.StatusOK {

				var err error

				// Read opening
				var b [1]byte
				for {
					if _, err = response.Body.Read(b[:]); err != nil {
						return
					}
					if b[0] == '[' {
						break
					} else if b[0] != ' ' && b[0] != 0xa {
						return
					}
				}

				decoder := utils.NewJSONDecoder(response.Body)
				for decoder.More() {
					var item T
					if err := decoder.Decode(&item); err != nil {
						return
					} else {
						result <- item
					}
				}
			}
		}
	}()

	return result
}

func getSideBlocksStreamFromAPI(method string) <-chan *index.SideBlock {
	return getStreamFromAPI[*index.SideBlock](method)
}

func getSideBlocksFromAPI(method string, cacheTime ...int) []*index.SideBlock {
	return getSliceFromAPI[*index.SideBlock](method, cacheTime...)
}

func getFromAPIRaw(method string, cacheTime ...int) []byte {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	return cacheResult[[]byte](method, time.Second*time.Duration(cTime), func() []byte {
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return nil
		} else {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				if data, err := io.ReadAll(response.Body); err != nil {
					return nil
				} else {
					return data
				}
			} else {
				return nil
			}
		}
	})
}
