package main

import (
	"github.com/rjeczalik/notify"
	"log"
	"regexp"
)

type watchResult struct {
	File  string
	Regex *regexp.Regexp
}

// watch a folder for changes
func watch(folder string, rs []*regexp.Regexp, result chan watchResult, done chan struct{}) {

	// Make the channel buffered to ensure no event is dropped. Notify will drop
	// an event if the receiver is not able to keep up the sending pace.
	c := make(chan notify.EventInfo)
	if err := notify.Watch(folder, c, notify.InCloseWrite); err != nil {
		log.Fatal(err)
	}
	defer notify.Stop(c)

	for {
		select {
		case ei := <-c:
			for _, r := range rs {
				if r.MatchString(ei.Path()) {
					log.Println("New war detected: ", ei.Path())
					result <- watchResult{ei.Path(), r}
					return
				}
			}
		case <-done:
			return
		}

	}
}
