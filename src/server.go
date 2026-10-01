package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	PG "github.com/jobinau/pgoperatorcalculator/src/pgoperatorcalculator"
)

func main() {
	address := flag.String("address", "0.0.0.0", "IP address or hostname to bind to")
	port := flag.Int("port", 8080, "TCP port to listen on")
	logLevel := flag.String("loglevel", "INFO", "log level: ERROR, INFO, or DEBUG")
	showVersion := flag.Bool("version", false, "print the calculator version and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "PostgreSQL Operator Calculator %s\n\n", PG.VERSION)
		fmt.Fprintln(flag.CommandLine.Output(), "Endpoints:")
		fmt.Fprintln(flag.CommandLine.Output(), "  GET  /supported   Return supported input values")
		fmt.Fprintln(flag.CommandLine.Output(), "  POST /calculator  Calculate a PostgreSQL configuration")
		fmt.Fprintln(flag.CommandLine.Output(), "\nFlags:")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(PG.VERSION)
		return
	}
	logger := newLogger(*logLevel)
	server := &http.Server{Addr: *address + ":" + strconv.Itoa(*port), Handler: routes(logger)}
	logger.Printf("starting PostgreSQL Operator Calculator on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		logger.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}

func newLogger(level string) *log.Logger {
	level = strings.ToUpper(level)
	if level != "ERROR" && level != "INFO" && level != "DEBUG" {
		level = "INFO"
	}
	return log.New(os.Stderr, "pgoperatorcalculator "+level+": ", log.LstdFlags)
}

func routes(logger *log.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/supported", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		var conf PG.Configuration
		conf.Init()
		writeJSON(w, http.StatusOK, conf)
	})
	mux.HandleFunc("/calculator", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		calculateRequest(w, r, logger)
	})
	return mux
}

func calculateRequest(w http.ResponseWriter, r *http.Request, logger *log.Logger) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to read request body: %v", err))
		return
	}
	var request PG.ConfigurationRequest
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "empty request body")
		return
	}
	if err := json.Unmarshal(body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "malformed JSON: "+err.Error())
		return
	}
	var conf PG.Configuration
	conf.Init()
	var calculator PG.PGOperatorCalculator
	calculator.Init(request, conf)
	calculationErr, message, families := calculator.GetCalculate()
	request = calculator.IncomingRequest
	status := http.StatusOK
	if message.MType == PG.ErrorexecI {
		status = http.StatusBadRequest
	} else if message.MType == PG.OverutilizingI {
		status = http.StatusUnprocessableEntity
	}
	if calculationErr != nil {
		logger.Printf("calculation returned type %d: %v", message.MType, calculationErr)
	}
	if request.Output == PG.ResultOutputFormatHuman {
		output, outputErr := calculator.GetHumanOutput(message, request, families)
		if outputErr != nil {
			writeError(w, http.StatusInternalServerError, outputErr.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(output.Bytes())
		return
	}
	output, outputErr := calculator.GetJSONOutput(message, request, families)
	if outputErr != nil {
		writeError(w, http.StatusInternalServerError, outputErr.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(output.Bytes())
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": text})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
