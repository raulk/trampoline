package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"

	"github.com/containerd/cgroups"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var data [][]byte // keep byte slabs

func main() {
	interactive := flag.Bool("interactive", false, "start in interactive HTTP mode")
	limit := flag.Int64("limit", 32, "memory limit in MiB")

	// delete the cgroup if it exists.
	if cgroup, err := cgroups.Load(cgroups.V1, cgroups.StaticPath("/trampoline")); err == nil {
		log.Printf("prexisting cgroup deleted")
		_ = cgroup.Delete()
	}

	// create the cgroup.
	cgroup, err := cgroups.New(cgroups.V1, cgroups.StaticPath("/trampoline"), &specs.LinuxResources{
		Memory: &specs.LinuxMemory{
			Limit: limit,
			Swap:  limit,
		},
	})
	if err != nil {
		panic(err)
	}
	defer cgroup.Delete()

	log.Printf("cgroup created: trampoline")

	if err := cgroup.Add(cgroups.Process{Pid: os.Getpid()}); err != nil {
		panic(fmt.Sprintf("failed to add process to group: %s", err))
	}

	log.Printf("process added to cgroup")

	if *interactive {
		interactiveMode()
		return
	}

}

func interactiveMode() {
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		bytes, err := parseBytes(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		add(bytes)

		_, _ = fmt.Fprintln(w, "added: ", bytes)
		_, _ = fmt.Fprintln(w)
		writeMemStats(w)
	})

	http.HandleFunc("/rel", func(w http.ResponseWriter, r *http.Request) {
		bytes, err := parseBytes(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		released, not := release(bytes)

		_, _ = fmt.Fprintln(w, "released: ", released)
		_, _ = fmt.Fprintln(w, "not released: ", not)
		_, _ = fmt.Fprintln(w)
		writeMemStats(w)
	})

	http.HandleFunc("/gc", gc)
	http.HandleFunc("/stats", stats)
	http.HandleFunc("/reset", reset)

	fmt.Println("http endpoint started")

	_ = http.ListenAndServe("0.0.0.0:1112", http.DefaultServeMux)

	return
}

func parseBytes(r *http.Request) (int, error) {
	bytesStr, ok := r.URL.Query()["bytes"]
	if !ok || len(bytesStr) == 0 {
		return -1, errors.New("invalid 'bytes' query parameter")
	}
	bytes, err := strconv.Atoi(bytesStr[0])
	if err != nil {
		return -1, err
	}
	return bytes, nil
}

func add(bytes int) {
	m := make([]byte, bytes)
	for j := range m {
		m[j] = 1
	}
	data = append(data, m)
}

func release(bytes int) (released, notReleased int) {
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
	return bytes - rem, rem
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
