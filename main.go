package main

import (
	"bytes"
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
)

type ConfigurationEntry struct {
	TomcatAddr string `json:"tomcat"`
	Username   string
	Password   string
	Folder     string
	Regex      string
}

// encode username and password
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func deployWar(addr, user, pass, warpath string) {
	log.Println("Start deploying: ", warpath, "to", addr)

	basename := path.Base(warpath)
	deployPath := basename[:strings.Index(basename, ".")]
	url := fmt.Sprintf("%s/manager/text/deploy?path=/%s&update=true", addr, deployPath)

	var defaultTtransport http.RoundTripper = &http.Transport{Proxy: nil}
	client := &http.Client{Transport: defaultTtransport}

	body, err := ioutil.ReadFile(warpath)
	if err != nil {
		log.Fatal("Error reading war: ", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Basic %s", basicAuth(user, pass)))
	resp, err := client.Do(req)
	log.Println("War deployed. Status code: ", resp.Status)
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
				deployWar(data.TomcatAddr, data.Username, data.Password, ei.Path())
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

	var wg sync.WaitGroup
	result := make(chan ConfigurationEntry)
	done := make(chan struct{})
	doneWorkers := make(chan struct{})

	go func(done chan struct{}) {
		for {
			select {
			case configurationEntry := <-result:
				// once a worker finished deploying respawn it
				log.Println("Spawn worker for", configurationEntry.Folder)
				wg.Add(1)
				go func() {
					watch(configurationEntry, result, doneWorkers)
					wg.Done()
				}()
			case <-done:
				close(doneWorkers)
				wg.Wait()
				return
			}
		}
	}(done)

	// start initial workers
	for _, configurationEntry := range configuration {
		go watch(configurationEntry, result, doneWorkers)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	wg.Add(1)
	go func() {
		<-c
		log.Println("Control-C catched. Waiting for workers to exit..")
		close(done)
		wg.Done()
	}()

	wg.Wait()
}
