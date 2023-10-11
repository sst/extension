// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const defaultListenerPort = "4323"
const initialQueueSize = 5

var httpServer *http.Server
var Events chan Event

type UnknownEvent struct {
	Time   string          `json:"time"`
	Type   string          `json:"type"`
	Record json.RawMessage `json:"record"`
}

type Event struct {
	Time   string
	Type   string
	Record interface{}
}

type PlatformInitStartEvent struct {
	InitializationType string `json:"initializationType"`
	Phase              string `json:"phase"`
	RuntimeVersion     string `json:"runtimeVersion"`
	RuntimeVersionArn  string `json:"runtimeVersionArn"`
}

type PlatformStartEvent struct {
	RequestID string `json:"requestId"`
	Version   string `json:"version"`
}

type PlatformRuntimeDone struct {
	RequestID string `json:"requestId"`
	Metrics   struct {
		DurationMs float64 `json:"durationMs"`
	} `json:"metrics"`
}

type PlatformReportEvent struct {
	RequestID string `json:"requestId"`
	Metrics   struct {
		DurationMs       float64 `json:"durationMs"`
		BilledDurationMs int64   `json:"billedDurationMs"`
		MemorySizeMb     int64   `json:"memorySizeMb"`
		MaxMemoryUsedMb  int64   `json:"maxMemoryUsedMb"`
		InitDurationMs   int64   `json:"initDurationMs"`
	} `json:"metrics"`
}

type FunctionEvent string

// Starts the server in a goroutine where the log events will be sent
func Start() (string, error) {
	address := "sandbox:" + defaultListenerPort
	httpServer = &http.Server{Addr: address}
	Events = make(chan Event, 1000)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println("[listener:http_handler] Error reading body:", err)
			return
		}

		// Parse and put the log messages into the queue
		var slice []UnknownEvent
		_ = json.Unmarshal(body, &slice)

		for _, evt := range slice {
			switch evt.Type {
			case "platform.initStart":
				var specific PlatformInitStartEvent
				_ = json.Unmarshal(evt.Record, &specific)
				Events <- Event{
					Time:   evt.Time,
					Type:   evt.Type,
					Record: specific,
				}
				break
			case "function":
				var specific string
				_ = json.Unmarshal(evt.Record, &specific)
				Events <- Event{
					Time:   evt.Time,
					Type:   evt.Type,
					Record: FunctionEvent(specific),
				}
				break
			case "platform.start":
				var specific PlatformStartEvent
				_ = json.Unmarshal(evt.Record, &specific)
				Events <- Event{
					Time:   evt.Time,
					Type:   evt.Type,
					Record: specific,
				}
				break
			case "platform.report":
				var specific PlatformReportEvent
				_ = json.Unmarshal(evt.Record, &specific)
				Events <- Event{
					Time:   evt.Time,
					Type:   evt.Type,
					Record: specific,
				}
				break
			case "platform.runtimeDone":
				var specific PlatformRuntimeDone
				log.Println(string(evt.Record))
				_ = json.Unmarshal(evt.Record, &specific)
				Events <- Event{
					Time:   evt.Time,
					Type:   evt.Type,
					Record: specific,
				}
				break
			case "platform.extension":
			case "platform.initReport":
			case "platform.initRuntimeDone":
			case "platform.telemetrySubscription":
				break
			default:
				log.Println("unknown event type", evt.Type, string(evt.Record))
			}
		}

		slice = nil
	})

	go func() {
		err := httpServer.ListenAndServe()
		if err != http.ErrServerClosed {
			log.Println("[listener:goroutine] Unexpected stop on Http Server:", err)
			Shutdown()
		} else {
			log.Println("[listener:goroutine] Http Server closed:", err)
		}
	}()

	return fmt.Sprintf("http://%s/", address), nil
}

// Terminates the HTTP server listening for logs
func Shutdown() {
	if httpServer != nil {
		ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
		err := httpServer.Shutdown(ctx)
		close(Events)

		if err != nil {
			log.Println("[listener:Shutdown] Failed to shutdown http server gracefully:", err)
		} else {
			httpServer = nil
		}
	}
}
