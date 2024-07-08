/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/containerd/containerd"
	_ "github.com/containerd/containerd/api/events" // Register grpc event types
	"github.com/containerd/containerd/events"
	"github.com/containerd/log"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/formatter"
	"github.com/containerd/typeurl/v2"
)

// EventOut contains information about an event.
type EventOut struct {
	Timestamp time.Time
	Namespace string
	Topic     string
	Status    Status
	Event     string
}

type Status string

const (
	START Status = "START"
)

func isStatus(status string) bool {
	status = strings.ToUpper(status)
	if string(START) != status {
		return false
	}

	return true
}

func parseFilter(filter string) (string, string, error) {
	filterSplit := strings.SplitN(filter, "=", 2)
	if len(filterSplit) != 2 {
		return "", "", fmt.Errorf("%s is an invalid filter", filter)
	}
	return filterSplit[0], filterSplit[1], nil
}

// EventFilter for filtering events
type EventFilter func(*EventOut) bool

func TopicToStatus(topic string) Status {
	if strings.Contains(strings.ToUpper(topic), string(START)) {
		return START
	}

	return "Unsupported Status"
}

func generateEventFilter(filter, filterValue string) (func(e *EventOut) bool, error) {
	switch strings.ToUpper(filter) {
	case "EVENT", "STATUS":
		//fmt.Print("event filter activated =====\n\n")
		// if filterValue == "die" { // Docker compat
		// 	filterValue = "died"
		// }
		return func(e *EventOut) bool {
			//fmt.Printf("checking if event is %s =======\n\n", filterValue)
			if !isStatus(string(e.Status)) {
				//fmt.Printf("event is not a status")
				return false
			}

			//fmt.Printf("\t is %s == %s ?\n", string(e.Status), strings.ToUpper(filterValue))
			return strings.ToUpper(string(e.Status)) == strings.ToUpper(filterValue)
		}, nil
	}

	return nil, fmt.Errorf("%s is an invalid or unsupported filter", filter)
}

func generateEventFilters(filters []string) (map[string][]EventFilter, error) {
	filterMap := make(map[string][]EventFilter)
	for _, filter := range filters {
		key, val, err := parseFilter(filter)
		if err != nil {
			return nil, err
		}
		filterFunc, err := generateEventFilter(key, val)
		if err != nil {
			return nil, err
		}
		filterSlice := filterMap[key]
		filterSlice = append(filterSlice, filterFunc)
		filterMap[key] = filterSlice
	}

	return filterMap, nil
}

func applyFilters(event *EventOut, filterMap map[string][]EventFilter) bool {
	for _, filters := range filterMap {
		match := false
		for _, filter := range filters {
			if filter(event) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}

// Events is from https://github.com/containerd/containerd/blob/v1.4.3/cmd/ctr/commands/events/events.go
func Events(ctx context.Context, client *containerd.Client, options types.SystemEventsOptions) error {
	eventsClient := client.EventService()
	eventsCh, errCh := eventsClient.Subscribe(ctx)
	var tmpl *template.Template
	switch options.Format {
	case "":
		tmpl = nil
	case "raw", "table", "wide":
		return errors.New("unsupported format: \"raw\", \"table\", and \"wide\"")
	default:
		var err error
		tmpl, err = formatter.ParseTemplate(options.Format)
		if err != nil {
			return err
		}
	}
	filterMap, err := generateEventFilters(options.Filters)
	if err != nil {
		return err
	}
	for {
		var e *events.Envelope
		select {
		case e = <-eventsCh:
		case err := <-errCh:
			return err
		}
		if e != nil {
			var out []byte
			if e.Event != nil {
				v, err := typeurl.UnmarshalAny(e.Event)
				if err != nil {
					log.G(ctx).WithError(err).Warn("cannot unmarshal an event from Any")
					continue
				}
				out, err = json.Marshal(v)
				if err != nil {
					log.G(ctx).WithError(err).Warn("cannot marshal Any into JSON")
					continue
				}
			}
			eOut := EventOut{e.Timestamp, e.Namespace, e.Topic, TopicToStatus(e.Topic), string(out)}
			match := applyFilters(&eOut, filterMap)
			//fmt.Printf("mathc is %s \n", match)
			if match {
				//fmt.Printf("\t IT MATCHES \n")
				if tmpl != nil {
					var b bytes.Buffer
					if err := tmpl.Execute(&b, eOut); err != nil {
						return err
					}
					if _, err := fmt.Fprintln(options.Stdout, b.String()+"\n"); err != nil {
						return err
					}
				} else {
					//fmt.Print("else block =====\n\n")
					if _, err := fmt.Fprintln(
						options.Stdout,
						e.Timestamp,
						e.Namespace,
						e.Topic,
						string(out),
					); err != nil {
						return err
					}
				}
			} else {
				//fmt.Printf("\t NO MATCH !! \n")
			} //DEBUG

		}
	}
}
