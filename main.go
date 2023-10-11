package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"github.com/sst/extension/api/extension"
	"github.com/sst/extension/api/telemetry"
	"github.com/sst/extension/server"
)

type Action struct {
	Action     string          `json:"action"`
	Properties json.RawMessage `json:"properties"`
}

type LogSplitAction struct {
	LogGroupName string `json:"logGroupName"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		cancel()
	}()

	extensionId, err := extension.Register(ctx)
	if err != nil {
		panic(err)
	}

	serverAddress, err := server.Start()
	if err != nil {
		panic(err)
	}

	telemetryApiClient := telemetry.NewClient()
	_, err = telemetryApiClient.Subscribe(ctx, extensionId, serverAddress)
	if err != nil {
		panic(err)
	}

	buffer := []string{}
	var logGroupName string

	cfg, _ := config.LoadDefaultConfig(ctx)
	client := cloudwatchlogs.NewFromConfig(cfg)
	streamName := fmt.Sprintf("%s/%s", time.Now().Format("2006/01/02"), uuid.New().String())
	pattern := regexp.MustCompile("::sst::(.+)")

	// Will block until invoke or shutdown event is received or cancelled via the context.
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// This is a blocking action
			res, err := extension.EventNext(ctx)
			if err != nil {
				log.Println("Exiting. Error:", err)
				return
			}
			logGroupName = ""
			buffer = []string{}

			if res.EventType == extension.Invoke {

			outerloop:
				for evt := range server.Events {
					switch v := evt.Record.(type) {
					case server.PlatformInitStartEvent:
						buffer = append(buffer, fmt.Sprintf("INIT_START Runtime Version: %s Runtime Version ARN: %s", v.RuntimeVersion, v.RuntimeVersionArn))
					case server.PlatformStartEvent:
						buffer = append(buffer, fmt.Sprintf("START RequestId: %s Version: %s", v.RequestID, v.Version))
					case server.FunctionEvent:
						matches := pattern.FindStringSubmatch(string(v))
						if len(matches) > 1 {
							var action Action
							err := json.Unmarshal([]byte(matches[1]), &action)
							if err != nil {
								continue
							}

							if action.Action != "log.split" {
								continue
							}

							var logSplitAction LogSplitAction
							err = json.Unmarshal(action.Properties, &logSplitAction)
							if err != nil {
								continue
							}

							logGroupName = logSplitAction.LogGroupName

							continue
						}
						buffer = append(buffer, string(v))
					case server.PlatformRuntimeDone:
						buffer = append(buffer, fmt.Sprintf("END RequestId: %s", v.RequestID))
						buffer = append(buffer, fmt.Sprintf("REPORT RequestId: %s	Duration: %v ms\tBilled Duration: %v ms\tMemory Size: %v MB\tMax Memory Used: %v MB", v.RequestID, v.Metrics.DurationMs, v.Metrics.DurationMs, os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"), 0))
						log.Println("flushing buffer")
						put := &cloudwatchlogs.PutLogEventsInput{
							LogGroupName:  aws.String(logGroupName),
							LogStreamName: aws.String(streamName),
							LogEvents:     []types.InputLogEvent{},
						}
						for _, message := range buffer {
							put.LogEvents = append(put.LogEvents, types.InputLogEvent{
								Message:   aws.String(message),
								Timestamp: aws.Int64(time.Now().UnixNano() / int64(time.Millisecond)),
							})
						}
						for {
							_, err := client.PutLogEvents(context.Background(), put)
							if err != nil {
								var apiErr smithy.APIError
								if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFoundException" {
									log.Println("Creating log group")
									_, err = client.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{
										LogGroupName: aws.String(logGroupName),
									})
									_, err = client.CreateLogStream(context.Background(), &cloudwatchlogs.CreateLogStreamInput{
										LogGroupName:  aws.String(logGroupName),
										LogStreamName: aws.String(streamName),
									})
									continue
								}
							}
							break
						}
						break outerloop
					}
				}
			} else if res.EventType == extension.Shutdown {
				// handle shutdown
				return
			}
		}
	}
}
