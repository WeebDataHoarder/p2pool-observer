package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

import jsoniter "github.com/json-iterator/go"

var json = jsoniter.ConfigCompatibleWithStandardLibrary

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
