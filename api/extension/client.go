// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// RegisterResponse is the body of the response for /register
type RegisterResponse struct {
	FunctionName    string `json:"functionName"`
	FunctionVersion string `json:"functionVersion"`
	Handler         string `json:"handler"`
}

// NextEventResponse is the response for /event/next
type NextEventResponse struct {
	EventType          EventType `json:"eventType"`
	DeadlineMs         int64     `json:"deadlineMs"`
	RequestID          string    `json:"requestId"`
	InvokedFunctionArn string    `json:"invokedFunctionArn"`
	Tracing            Tracing   `json:"tracing"`
}

// Tracing is part of the response for /event/next
type Tracing struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// StatusResponse is the body of the response for /init/error and /exit/error
type StatusResponse struct {
	Status string `json:"status"`
}

// EventType represents the type of events recieved from /event/next
type EventType string

const (
	Invoke                   EventType = "INVOKE"
	Shutdown                 EventType = "SHUTDOWN"
	extensionNameHeader                = "Lambda-Extension-Name"
	extensionIdentiferHeader           = "Lambda-Extension-Identifier"
	extensionErrorType                 = "Lambda-Extension-Function-Error-Type"
)

var baseUrl = fmt.Sprintf("http://%s/2020-01-01/extension", os.Getenv("AWS_LAMBDA_RUNTIME_API"))
var client = &http.Client{}
var extensionID string

// Registers the extension with Extensions API
func Register(ctx context.Context) (string, error) {
	url := baseUrl + "/register"

	body, err := json.Marshal(map[string]interface{}{
		"events": []EventType{Invoke, Shutdown},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Set(extensionNameHeader, "sst")

	res, err := client.Do(req)
	if err != nil {
		log.Println("[client:Register] Registration failed", err)
		return "", err
	}

	if res.StatusCode != 200 {
		log.Println("[client:Register] Registration failed with statusCode ", res)
		return "", fmt.Errorf("registration failed with status %s", res.Status)
	}

	defer res.Body.Close()
	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	out := RegisterResponse{}
	err = json.Unmarshal(bytes, &out)
	if err != nil {
		return "", err
	}

	extensionID = res.Header.Get(extensionIdentiferHeader)
	return extensionID, nil
}

// Blocks while long polling for the next Lambda invoke or shutdown
func EventNext(ctx context.Context) (*NextEventResponse, error) {
	url := baseUrl + "/event/next"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(extensionIdentiferHeader, extensionID)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("request failed with status %s", res.Status)
	}
	defer res.Body.Close()
	byes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	out := NextEventResponse{}
	err = json.Unmarshal(byes, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Reports an initialization error to the platform. Call it when you registered but failed to initialize
func InitError(errorType string) (*StatusResponse, error) {
	url := baseUrl + "/init/error"

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(extensionIdentiferHeader, extensionID)
	req.Header.Set(extensionErrorType, errorType)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("request failed with status %s", res.Status)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	out := StatusResponse{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Reports an error to the platform before exiting. Call it when you encounter an unexpected failure
func ExitError(errorType string) (*StatusResponse, error) {
	url := baseUrl + "/exit/error"

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(extensionIdentiferHeader, extensionID)
	req.Header.Set(extensionErrorType, errorType)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("request failed with status %s", res.Status)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	out := StatusResponse{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
