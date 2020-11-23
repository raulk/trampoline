package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"

	"github.com/containerd/cgroups"
	"github.com/fatih/color"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var data [][]byte // keep byte slabs

func main() {
	var (
		interactive = flag.Bool("interactive", false, "start in interactive HTTP mode")
		limit       = flag.Int64("limit", 32<<20, "memory limit in MiB")
		gc          = flag.Bool("gc", false, "run GC to prevent overallocation")
	)

	flag.Parse()

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

	var stats runtime.MemStats
	writeMemStats(&stats, log.Writer())

	var (
		reserved  = uint64(float64(*limit) * 0.10)
		available = uint64(*limit) - stats.HeapAlloc
		fill      = available - reserved
	)

	log.Printf(color.GreenString("starting situation"))
	log.Printf("available: %d, filling: %d, reserving: %d", available, fill, reserved)

	log.Printf(color.GreenString("allocating a slab of %d bytes + slice header", fill))

	// force the allocation on the heap.
	data = append(data, func() []byte {
		slab := make([]byte, fill)
		for i := range slab {
			slab[i] = 0xff
		}
		return slab
	}())

	log.Printf(color.GreenString("slab allocated"))

	writeMemStats(&stats, log.Writer())

	log.Printf("heap allocated: %d, gc planned for: %d, exceeding by: %d", stats.HeapAlloc, stats.NextGC, *limit-int64(stats.NextGC))

	log.Printf(color.GreenString("releasing the slab"))
	data = nil
	log.Printf("slab released; there is now sufficient memory available to reallocate")

	if *gc {
		log.Printf(color.GreenString("running GC"))
		runtime.GC()
		log.Printf("stats after GC")
		writeMemStats(&stats, log.Writer())
		log.Printf("GC ran, this program should not crash")
	}

	log.Printf(color.GreenString("now allocating a new slab for the reserved quantity"))

	// force the allocation on the heap.
	data = append(data, func() []byte {
		slab := make([]byte, reserved)
		for i := range slab {
			slab[i] = 0xff
		}
		return slab
	}())

	log.Printf(color.YellowString("Congratulations, this program did not crash!"))
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

		var stats runtime.MemStats
		writeMemStats(&stats, w)
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

		var stats runtime.MemStats
		writeMemStats(&stats, w)
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
	var stats runtime.MemStats
	writeMemStats(&stats, w)
}

func stats(w http.ResponseWriter, r *http.Request) {
	var stats runtime.MemStats
	writeMemStats(&stats, w)
}

func reset(w http.ResponseWriter, r *http.Request) {
	data = nil
	runtime.GC()
	var stats runtime.MemStats
	writeMemStats(&stats, w)
}

func writeMemStats(stats *runtime.MemStats, w io.Writer) {
	runtime.ReadMemStats(stats)
	_, _ = fmt.Fprintln(w, "memstats:")
	_, _ = fmt.Fprintln(w, "\tallocated:", stats.Alloc)
	_, _ = fmt.Fprintln(w, "\tmalloc objects:", stats.Mallocs)
	_, _ = fmt.Fprintln(w, "\tfreed objects:", stats.Frees)
	_, _ = fmt.Fprintln(w, "\theap alloc:", stats.HeapAlloc)
	_, _ = fmt.Fprintln(w, "\theap idle:", stats.HeapIdle)
	_, _ = fmt.Fprintln(w, "\theap objects:", stats.HeapObjects)
	_, _ = fmt.Fprintln(w, "\theap in-use:", stats.HeapInuse)
	_, _ = fmt.Fprintln(w, "\theap released:", stats.HeapReleased)
	_, _ = fmt.Fprintln(w, "\tlast gc:", stats.LastGC)
	_, _ = fmt.Fprintln(w, "\tnext gc:", stats.NextGC)
	_, _ = fmt.Fprintln(w, "\tnum gc:", stats.NumGC)
}
