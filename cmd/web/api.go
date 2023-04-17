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

func getSideBlockFromAPI(method string, cacheTime ...int) *index.SideBlock {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	if sb, ok := cacheResult(method, time.Second*time.Duration(cTime), func() any {
		uri, _ := url.Parse(os.Getenv("API_URL") + method)
		if response, err := http.DefaultClient.Do(&http.Request{
			Method: "GET",
			URL:    uri,
		}); err != nil {
			return nil
		} else {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				var sideBlock index.SideBlock
				if data, err := io.ReadAll(response.Body); err != nil {
					return nil
				} else if json.Unmarshal(data, &sideBlock) != nil {
					return nil
				} else {
					return &sideBlock
				}
			} else {
				return nil
			}
		}
	}).(*index.SideBlock); ok {
		return sb
	}
	return nil
}

func getSideBlocksFromAPI(method string, cacheTime ...int) []*index.SideBlock {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	if sb, ok := cacheResult(method, time.Second*time.Duration(cTime), func() any {
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
	}).([]*index.SideBlock); ok {
		return sb
	}
	return nil
}

func getFromAPIRaw(method string, cacheTime ...int) []byte {
	cTime := 0
	if len(cacheTime) > 0 {
		cTime = cacheTime[0]
	}

	if b, ok := cacheResult(method, time.Second*time.Duration(cTime), func() any {
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
	}).([]byte); ok {
		return b
	}
	return nil
}
