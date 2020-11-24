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
	"runtime/debug"
	"strconv"

	"github.com/containerd/cgroups"
	"github.com/fatih/color"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// data stores byte slabs that are retained in heap.
var data [][]byte

// This program has two modes of running: self-directed (default), or interactive.
//
// In self-directed mode, it creates a cgroup and enforces the memory limit indicated by the -limit parameter (default: 32MiB).
// The cgroup's swap memory value is set to the same value, so as to prevent the program from using any swap.
//
// IMPORTANT: cgroups is only available on Linux kernels, and for this to work properly, you will need to enable the
// "cgroup_enable=memory swapaccount=1" kernel options. In Ubuntu, this is done by:
//
//  1. appending that string to the GRUB_CMDLINE_LINUX option in /etc/default/grub
//  2. running sudo update-grub
//  3. rebooting the host
//
// This program will allocate a byte slice of size 90% of the limit (+ slice overhead).
// This will simulate a spike heap usage, and will very likely induce GC at around 30MiB (with the default limit value).
//
// Of course, the exact numbers are dependent on many conditions, and thus non-deterministic.
// Could be less or more in your setup, and you may need to tweak the limit parameter.
//
// Given the default value of GOGC=100, the GC pacer will schedule to run when the allocated
// heap amounts to 2x of the live set at GC mark phase end. In my setup, this clocks in at 60MiB.
// Of course, that's beyond our 32MiB limit.
//
// Next, the program releases the 90% byte slab, and allocates the remaining 10%.
// With the default limit value, it releases 30198988 bytes to allocate 3355443 bytes (obviating slice headers).
//
// At that point, the program has enough unused heap space that it could reclaim and
// assign to the new allocation. But unfortunately, GC is scheduled too far out,
// and the Go runtime does not run GC as a last resource before going above its
// limit. Therefore, instead of reusing vacant, resident memory, it decides to expand
// the heap and goes beyond its cgroup limit, thus triggering the OOM killer.
//
// The gist here is that the Go runtime had 9x times (roughly) as much memory free as it needed
// to allocate, but it was not capable of reclaiming it in time.
func main() {
	var (
		interactive = flag.Bool("interactive", false, "start in interactive HTTP mode")
		limit       = flag.Int64("limit", 32<<20, "memory limit in MiB")
		gc          = flag.Bool("gc", false, "run GC to prevent overallocation")
	)

	flag.Parse()

	ch := make(chan struct{}, 1)
	go func() {
		for range ch {
			log.Println("received memory pressure notification")
		}
	}()
	maxHeap := uintptr(*limit)
	fmt.Println("setting max heap to:", maxHeap)
	debug.SetMaxHeap(maxHeap, ch)

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

// interactiveMode places this program in interactive HTTP mode. This will expose
// an HTTP endpoint on 0.0.0.0:1112, with 5 routes:
//
// * /add?bytes=n, to add a byte slab of the specified amount to the heap.
// * /rel?bytes=n, to release as many bytes as specified.
// * /gc, to trigger GC.
// * /stats, to get memory stats.
// * /reset, to clear all retained byte slabs.
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
