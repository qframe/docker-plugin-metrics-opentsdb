package main

import (
	"encoding/json"
	"log"
	"time"
	"net"
	"net/http"
	"sync"
	"fmt"
	"github.com/tv42/httpunix"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prom2json"
	"github.com/qnib/prom2all"
	"github.com/docker/go-plugins-helpers/sdk"

	"os"
)

const (
	version = "0.1.0"
)

var (
	started bool
	mu 		sync.Mutex
	mfChan  chan *dto.MetricFamily

)

func Pusher() {
	host := os.Getenv("OPENTSDB_HOST")
	port := os.Getenv("OPENTSDB_PORT")
	addr := fmt.Sprintf("%s:%s", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Println(err.Error())
	}
	for mf := range mfChan {
		f := prom2json.NewFamily(mf)
		hostname, err := os.Hostname()
		if err == nil {
			f.AddLabel("hostname", hostname)
		}
		msgs := prom2all.ToOpenTSDBv1(f)
		for _, msg := range msgs {
			if os.Getenv("DRY_RUN") != "true" {
				fmt.Fprintf(conn,  msg + "\n")
			} else {
				fmt.Printf(msg + "\n")
			}
		}
	}

}

func main() {
	fmt.Printf(">>>> Start plugin v%s\n", version)
	mfChan = make(chan *dto.MetricFamily, 1024)
	go Pusher()
	h := sdk.NewHandler(`{"Implements": ["MetricsCollector"]}`)
	handlers(&h)
	if err := h.ServeUnix("metrics", 0); err != nil {
		panic(err)
	}
}

func PushForward() {
	ticker := time.NewTicker(time.Duration(2)*time.Second).C
	for {
		select {
		case <- ticker:
			u := &httpunix.Transport{
				DialTimeout:           100 * time.Millisecond,
				RequestTimeout:        500 * time.Millisecond,
				ResponseHeaderTimeout: 500 * time.Millisecond,
			}
			u.RegisterLocation("docker", "/run/docker/metrics.sock")

			var client = http.Client{
				Transport: u,
			}

			resp, err := client.Get("http+unix://docker/metrics")
			if err != nil {
				log.Fatal(err)
			}
			prom2json.ParseResponse(resp, mfChan)
		}
	}
}

func handlers(h *sdk.Handler) {
	h.HandleFunc("/MetricsCollector.StartMetrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(">>>>>>> Got /MetricsCollector.StartMetrics")
		var err error
		defer func() {
			var res struct{ Err string }
			if err != nil {
				res.Err = err.Error()
			}
			json.NewEncoder(w).Encode(&res)
		}()
		mu.Lock()
		defer mu.Unlock()
		if ! started {
			started = true
			go PushForward()

		}
	})

	h.HandleFunc("/MetricsCollector.StopMetrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(">>>>>>> Got /MetricsCollector.StopMetrics")
		json.NewEncoder(w).Encode(map[string]string{})
	})
}
