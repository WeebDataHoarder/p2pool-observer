package main

import (
	"encoding/json"
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
