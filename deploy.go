package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"strings"
)

// encode username and password
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func deployWar(addr, user, pass, appname, warpath string, done chan chan struct{}) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	log.Println("Start deploying: ", warpath, "to", addr)

	if len(appname) == 0 {
		basename := path.Base(warpath)
		appname = basename[:strings.Index(basename, ".")]
	}
	url := fmt.Sprintf("%s/manager/text/deploy?path=/%s&update=true", addr, appname)

	var defaultTtransport http.RoundTripper = &http.Transport{Proxy: nil}
	client := &http.Client{Transport: defaultTtransport}

	body, err := ioutil.ReadFile(warpath)
	if err != nil {
		log.Fatal("Error reading war: ", err)
	}

	// create context
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Create the request with context to be able to cancel it
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewBuffer(body))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", fmt.Sprintf("Basic %s", basicAuth(user, pass)))

	c := make(chan *http.Response, 1)
	go func() {
		resp, _ := client.Do(req)
		c <- resp
	}()

	select {
	case r := <-c:
		if r.StatusCode != 200 {
			log.Println("War deployed failed. Status: ", r.Status)
		} else {
			log.Printf("War deployed under '%s/%s/' . Status code: %s\n", addr, appname, r.Status)
		}
	case responseCh := <-done:
		log.Println("Deploy canceled.")
		cancel()
		responseCh <- struct{}{}
	}
}
