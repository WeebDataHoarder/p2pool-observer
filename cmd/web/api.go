package main

import (
	"encoding/json"
	"git.gammaspectra.live/P2Pool/p2pool-observer/index"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func getFromAPI(method string, cacheTime ...int) any {

	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	return cacheResult(method, time.Second*time.Duration(cTime), func() any {
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return nil
		} else {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				if strings.Index(response.Header.Get("content-type"), "/json") != -1 {
					var result any
					decoder := json.NewDecoder(response.Body)
					decoder.UseNumber()
					err = decoder.Decode(&result)
					return result
				} else if data, err := io.ReadAll(response.Body); err != nil {
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
				if data, err := io.ReadAll(response.Body); err != nil {
					return nil
				} else if json.Unmarshal(data, &result) != nil {
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

func getSideBlocksFromAPI(method string, cacheTime ...int) []*index.SideBlock {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	return cacheResult[[]*index.SideBlock](method, time.Second*time.Duration(cTime), func() []*index.SideBlock {
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return nil
		} else {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				var sideBlocks []*index.SideBlock
				if data, err := io.ReadAll(response.Body); err != nil {
					return nil
				} else if json.Unmarshal(data, &sideBlocks) != nil {
					return nil
				} else {
					return sideBlocks
				}
			} else {
				return nil
			}
		}
	})
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
