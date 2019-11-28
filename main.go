package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/rjeczalik/notify"
)

var (
	configurationFile string
	configuration     []ConfigurationEntry

	// holds the hash of the in progress deploying and the canceling channel
	deployMap = make(map[string]chan chan struct{})
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

// watch a folder for changes and execute handleFunc when a regex is true
func watch(data ConfigurationEntry, result chan ConfigurationEntry, done chan struct{}) {

	// Make the channel buffered to ensure no event is dropped. Notify will drop
	// an event if the receiver is not able to keep up the sending pace.
	c := make(chan notify.EventInfo)
	if err := notify.Watch(data.Folder, c, notify.InCloseWrite); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	r := regexp.MustCompile(data.Regex)

	for {
		select {
		case ei := <-c:
			if r.MatchString(ei.Path()) {
				log.Println("New war detected: ", ei.Path())
				data.File = ei.Path()
				result <- data
				return
			}
		case <-done:
			return
		}

	}
}

func main() {
	// Read configuration
	flag.StringVar(&configurationFile, "config", "nodata", "JSON configuration file")
	flag.Parse()

	data, err := ioutil.ReadFile(configurationFile)
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(data, &configuration); err != nil {
		panic(err)
	}
	log.Println(spew.Sdump(configuration))

	result := make(chan ConfigurationEntry)
	done := make(chan chan struct{})
	doneWatcherCh := make(chan struct{})

	go func(done chan chan struct{}) {
		for {
			select {
			case configurationEntry := <-result:

				// check if deployment is in progress and cancel it if so
				if doneCh, ok := deployMap[configurationEntry.Hash()]; ok {
					log.Println("Deploy in progress. Canceling it...")
					responseCh := make(chan struct{})
					doneCh <- responseCh
					<-responseCh
				}

				// deploy the new war
				doneCh := make(chan chan struct{}, 1)
				go func() {
					deployWar(configurationEntry.TomcatAddr,
						configurationEntry.Username,
						configurationEntry.Password,
						configurationEntry.Appname,
						configurationEntry.File,
						doneCh)
					delete(deployMap, configurationEntry.Hash())
				}()
				deployMap[configurationEntry.Hash()] = doneCh

				// re spawn the watcher
				log.Println("Spawn worker for", configurationEntry.Folder)
				go func() {
					watch(configurationEntry, result, doneWatcherCh)
				}()

			case c := <-done:

				// close all watchers
				close(doneWatcherCh)

				// close in progress deployments
				for _, doneCh := range deployMap {
					log.Println("Close worker")
					c := make(chan struct{})
					doneCh <- c
					<-c
				}
				c <- struct{}{}
				return
			}
		}
	}(done)

	// start initial workers
	for _, configurationEntry := range configuration {
		go watch(configurationEntry, result, doneWatcherCh)
	}

	// catch Control+C signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		<-c
		log.Println("Control-C catched. Waiting for workers to exit..")

		waitCh := make(chan struct{})
		// close main go routine. Wait for watcher and deployments to be closed.
		done <- waitCh
		<-waitCh
		wg.Done()
	}()

	wg.Wait()
}
