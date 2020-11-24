# Experiments with Go's garbage collection

This program creates a cgroup and enforces the memory limit indicated by the
-limit parameter (default: 32MiB).

The cgroup's swap memory value is set to the same value, to prevent the program
from using any swap.

> IMPORTANT: cgroups is only available on Linux kernels, and for this to work
> properly, you will need to enable the "cgroup_enable=memory swapaccount=1"
> kernel options.
>  
> In Ubuntu, this is done by:
> 
>  1. appending that string to the GRUB_CMDLINE_LINUX option in /etc/default/grub.
>  2. running sudo update-grub.
>  3. rebooting the host.

This program will then allocate a byte slice of size 90% of the configured limit
(+ slice overhead). This will simulate a spike in heap usage, and will very
likely induce GC at around 30MiB (with the default limit value).

Of course, the exact numbers are dependent on many conditions, and thus
non-deterministic. Could be less or more in your setup, and you may need to
tweak the limit parameter.

Given the default value of GOGC=100, the GC pacer will schedule to run when the
allocated heap amounts to 2x of the live set at GC mark phase end. In my setup,
this clocks in at 60MiB. Of course, that's beyond our 32MiB limit.

Next, the program releases the 90% byte slab, and allocates the remaining 10%.
With the default limit value, it releases 30198988 bytes to allocate 3355443
bytes (obviating slice headers).

At that point, the program has enough unused heap space that it could reclaim
and assign to the new allocation. But unfortunately, GC is scheduled too far
out, and the Go runtime does not run GC as a last resource before going above
its limit. Therefore, instead of reusing vacant, resident memory, it decides to
expand the heap and goes beyond its cgroup limit, thus triggering the OOM killer.

The gist here is that the Go runtime had 9x times (roughly) as much memory free
as it needed to allocate, but it was not capable of reclaiming it in time.

## Self-directed and interactive modes 

This program has two modes of running: self-directed (default), or interactive,
to enable experimentation.

In self-directed mode, the program will do what is explained above.

In interactive mode, the program will expose HTTP endpoint on 0.0.0.0:1112,
with 5 routes, and will not do any self-driven allocation.

 * /add?bytes=n, to add a byte slab of the specified amount to the heap.
 * /rel?bytes=n, to release as many bytes as specified.
 * /gc, to trigger GC.
 * /stats, to get memory stats.
 * /reset, to clear all retained byte slabs.

## Author & license

@raulk. MIT and ASFv2 licensed.