package main

import (
	"testing"
	"time"
)

func TestAppCloseWaitsForActiveRequest(t *testing.T) {
	app := &App{}
	if !app.beginRequest() {
		t.Fatal("app rejected request before shutdown")
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- app.Close() }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		app.lifecycleMu.Lock()
		closing := app.closing
		app.lifecycleMu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("App.Close did not enter closing state")
		}
		time.Sleep(time.Millisecond)
	}
	if app.beginRequest() {
		app.endRequest()
		t.Fatal("app accepted a new request during shutdown")
	}
	select {
	case err := <-closeResult:
		t.Fatalf("App.Close returned while request active: %v", err)
	default:
	}
	app.endRequest()
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("App.Close returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("App.Close did not return after request completed")
	}
}
