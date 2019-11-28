package main

import (
	"encoding/json"
	"flag"
	"github.com/davecgh/go-spew/spew"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"sync"
)

var (
	configurationFile string
	config            configurations

	// holds the hash of the in progress deploying and the canceling channel
	deployMap = make(map[string]chan chan struct{})
)

func main() {
	// Read configuration
	flag.StringVar(&configurationFile, "config", "nodata", "JSON configuration file")
	flag.Parse()

	data, err := ioutil.ReadFile(configurationFile)
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		panic(err)
	}
	log.Println(spew.Sdump(config))

	result := make(chan watchResult)
	done := make(chan chan struct{})
	doneWatcherCh := make(chan struct{})

	go func(done chan chan struct{}) {
		for {
			select {
			case r := <-result:
				configurationEntry := config.getConfigurationEntry(r.Regex)

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
						r.File,
						doneCh)
					delete(deployMap, configurationEntry.Hash())
				}()
				deployMap[configurationEntry.Hash()] = doneCh

				// re spawn the watcher
				log.Println("Spawn worker for", configurationEntry.Folder)
				go func() {
					watch(configurationEntry.Folder, config.getRegex(configurationEntry.Folder), result, doneWatcherCh)
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
	for folder, regex := range config.mapFolderToRegex() {
		go watch(folder, regex, result, doneWatcherCh)
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
