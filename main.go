package main

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
)

var data [][]byte // keep byte slabs

func main() {
	http.HandleFunc("/add", add)
	http.HandleFunc("/release", release)
	http.HandleFunc("/gc", gc)
	http.HandleFunc("/stats", stats)
	http.HandleFunc("/reset", reset)

	fmt.Println("started")

	_ = http.ListenAndServe("0.0.0.0:1112", http.DefaultServeMux)
}

func add(w http.ResponseWriter, r *http.Request) {
	bytesStr, ok := r.URL.Query()["bytes"]
	if !ok || len(bytesStr) == 0 {
		http.Error(w, "please specify how many bytes to allocate in the 'bytes' query parameter", http.StatusBadRequest)
		return
	}

	bytes, err := strconv.Atoi(bytesStr[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m := make([]byte, bytes)
	for j := range m {
		m[j] = 1
	}
	data = append(data, m)

	_, _ = fmt.Fprintln(w, "added: ", bytes)
	_, _ = fmt.Fprintln(w)

	writeMemStats(w)
}

func release(w http.ResponseWriter, r *http.Request) {
	bytesStr, ok := r.URL.Query()["bytes"]
	if !ok || len(bytesStr) == 0 {
		http.Error(w, "please specify how many bytes to allocate in the 'bytes' query parameter", http.StatusBadRequest)
		return
	}

	bytes, err := strconv.Atoi(bytesStr[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rem := bytes
	for i := 0; i < len(data) && rem > 0; i++ {
		head := data[i]
		if l := len(head); rem >= l {
			data[i] = nil
			rem -= l
		} else {
			slice := make([]byte, len(head)-rem)
			copy(slice, head)
			data[i] = slice
			rem = 0
		}
	}

	_, _ = fmt.Fprintln(w, "released: ", bytes-rem)
	_, _ = fmt.Fprintln(w, "not released: ", rem)
	_, _ = fmt.Fprintln(w)

	writeMemStats(w)
}

func gc(w http.ResponseWriter, r *http.Request) {
	runtime.GC()
	writeMemStats(w)
}

func stats(w http.ResponseWriter, r *http.Request) {
	writeMemStats(w)
}

func reset(w http.ResponseWriter, r *http.Request) {
	data = nil
	runtime.GC()
	writeMemStats(w)
}

func writeMemStats(w http.ResponseWriter) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	_, _ = fmt.Fprintln(w, "allocated: ", stats.Alloc)
	_, _ = fmt.Fprintln(w, "malloc objects: ", stats.Mallocs)
	_, _ = fmt.Fprintln(w, "freed objects: ", stats.Frees)
	_, _ = fmt.Fprintln(w, "heap alloc: ", stats.HeapAlloc)
	_, _ = fmt.Fprintln(w, "heap idle: ", stats.HeapIdle)
	_, _ = fmt.Fprintln(w, "heap objects: ", stats.HeapObjects)
	_, _ = fmt.Fprintln(w, "heap in-use: ", stats.HeapInuse)
	_, _ = fmt.Fprintln(w, "heap released: ", stats.HeapReleased)
	_, _ = fmt.Fprintln(w, "last gc: ", stats.LastGC)
	_, _ = fmt.Fprintln(w, "next gc: ", stats.NextGC)
	_, _ = fmt.Fprintln(w, "num gc: ", stats.NumGC)
}
