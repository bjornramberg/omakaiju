# Under the Hood

## Footprint & Performance

Measured on the v0.10.0 release build (stripped, static) on an idle Linux desktop:

| Metric              | Measurement                       |
| ------------------- | --------------------------------- |
| **Binary Size**     | ~9.4 MiB (stripped)               |
| **Resident Memory** | ~19 MB                            |
| **Startup Time**    | ~35 ms                            |
| **Dependencies**    | 0 runtime dependencies, 0 daemons |

> **Note on Binary Size:** The majority of the binary weight comes from Chroma's syntax highlighting engine and the Go runtime.

The size is the released file, the memory is `VmRSS` from `/proc/<pid>/status`
while a directory is listed, and the startup time is how long the process
takes to reach its first frame. The memory figure moves with the size of the
directory being listed and with the theme in use.

## Previews & Asset Preloading

* **Text files:** Chroma syntax highlighting with line numbers.
* **Binaries:** Automatic hex dump view.
* **Archives:** Full table-of-contents listings for `.zip`, `.tar`, and `.tgz`.
* **Images:** Metadata-only preview to keep memory usage minimal.

---

## File operations are safe by construction

Copies and moves never expose a half-written file, and never lose one.

* A copy is written to a scratch file beside the destination and renamed into
  place only once it is whole, flushed and checked against the source. The
  destination you already had is replaced atomically, or not at all — and it
  is held aside until the replacement is confirmed, so a replacement that
  does not land puts the old file back.
* A failed or cancelled copy does not put a half-written file in place, and
  nothing it made for itself is left lying around. In a folder copy the files
  it had already finished stay: they are whole, and they are the ones the
  copy was asked for.
* A copy keeps the file's mode — including the setuid, setgid and sticky
  bits — and its modification and access times. A copied directory gets the
  source's mode and times once its contents are in place, so the umask cannot
  mask them and writing the tree does not make the directory look newer.
  Ownership, ACLs, extended attributes and sparseness are not carried over:
  a sparse file arrives with all its blocks.
* A move between filesystems — onto an SMB share, say — is a rename the kernel
  refuses, so omakaiju copies the file across and removes the original. The
  original is only removed once the copy has landed and been verified.
* A move never replaces a file that is already there. Renaming onto a name
  that is taken is refused, and the file under that name is left alone. The
  one case the kernel cannot make safe is another program writing to that
  exact name in the same instant.
* A batch of moves that would put two entries under the same name is refused
  before anything moves, so one entry can never quietly overwrite another.
* Each entry of a move is moved on its own: it lands only once its
  destination is safely in place, and it keeps its source until then. A batch
  can therefore finish partly — if one entry fails, the entries that already
  landed stay moved, and the report names what failed and what did not. What
  did not move stays selected, so it can be tried again.
* A source whose size or timestamp changed while it was on its way is left
  alone: it is not the file that arrived.
* Only regular files are copied. A pipe, a socket or a device is reported by
  name and left where it is, rather than copied.
* A file reached by two paths (a symlinked directory, a share mounted in two
  places) is recognised as one file, so copying or moving it onto itself is
  refused instead of emptying it.
* Creating a file never empties one that is already there.
* Every operation that fails says so, names the file, and stays on screen
  until you do something else. What did not move stays selected, so you can
  fix the cause and try again.

## Debug pane (hidden)

`Shift+B` toggles a debug pane in the bottom-right corner, underneath the file
preview. It is deliberately not listed in the keybinding tables above.

The pane reports the release version, process id, heap in use and total memory
taken from the OS, goroutine and GC counts, how long the app has been running
and when it started, how long the startup itself took, the active pane's
path, entry count and cursor, the marked count, sort mode and filter, the
previewed path, the theme path, and the terminal size.

The startup is measured where the bytes leave: from the process starting to
the first frame reaching the terminal, which is the wait a user actually
experiences. It includes the time the runtime spent starting up and the
time the terminal took to answer the capability queries the renderer asks
on the way in. It samples the runtime every
500 ms — twice a second — and only while it is open: closing it leaves no
ticker, goroutine or sampler running. In a terminal too short to give both the
preview and the pane three rows, the pane stays hidden and the preview keeps
the whole column. With the preview hidden, the pane appears alone in the right
column.
