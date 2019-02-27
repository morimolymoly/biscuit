package main

import "fmt"
import "math/rand"
import "runtime"
import "strings"
import "sync/atomic"
import "sync"
import "unsafe"

type trapstore_t struct {
	trapno    int
	pid       int
	faultaddr int
	tf	  [TFSIZE]int
}
const maxtstore int = 64
const maxcpus   int = 32

//go:nosplit
func tsnext(c int) int {
	return (c + 1) % maxtstore
}

var	numcpus	int = 1

type cpu_t struct {
	num		int
	// per-cpus interrupt queues. the cpu interrupt handler is the
	// producer, the go routine running trap() below is the consumer. each
	// cpus interrupt handler increments head while the go routine consuer
	// increments tail
	trapstore	[maxtstore]trapstore_t
	tshead		int
	tstail		int
}

var cpus	[maxcpus]cpu_t

// can only be used when interrupts are cleared
//go:nosplit
func lap_id() int {
	lapaddr := (*[1024]int32)(unsafe.Pointer(uintptr(0xfee00000)))
	return int(lapaddr[0x20/4] >> 24)
}

const(
	GPFAULT   = 13
	PGFAULT   = 14
	TIMER     = 32
	SYSCALL   = 64

	// low 3 bits must be zero
	IRQ_BASE  = 32
	IRQ_KBD = 1
	IRQ_DISK  = 14
	IRQ_LAST  = IRQ_BASE + 16
	INT_DISK  = IRQ_BASE + IRQ_DISK
	INT_KBD = IRQ_BASE + IRQ_KBD
)

// trap cannot do anything that may have side-effects on the runtime (like
// fmt.Print, or use panic!). the reason is that goroutines are scheduled
// cooperatively in the runtime. trap interrupts the runtime though, and then
// tries to execute more gocode on the same M, thus doing things the runtime
// did not expect.
//go:nosplit
func trapstub(tf *[TFSIZE]int, pid int) {

	trapno := tf[TF_TRAP]

	// kernel faults are fatal errors for now, but they could be handled by
	// trap & c.
	if pid == 0 && trapno < IRQ_BASE {
		runtime.Pnum(trapno)
		if trapno == PGFAULT {
			runtime.Pnum(runtime.Rcr2())
		}
		rip := tf[TF_RIP]
		runtime.Pnum(rip)
		runtime.Pnum(0x42)
		runtime.Pnum(lap_id())
		runtime.Tfdump(tf)
		//runtime.Stackdump(tf[TF_RSP])
		for {}
	}

	lid := cpus[lap_id()].num
	head := cpus[lid].tshead
	tail := cpus[lid].tstail

	// add to trap circular buffer for actual trap handler
	if tsnext(head) == tail {
		runtime.Pnum(0xbad)
		for {}
	}
	ts := &cpus[lid].trapstore[head]
	ts.trapno = trapno
	ts.pid = pid
	ts.tf = *tf
	if trapno == PGFAULT {
		ts.faultaddr = runtime.Rcr2()
	}

	// commit interrupt
	head = tsnext(head)
	cpus[lid].tshead = head

	switch trapno {
	case SYSCALL, PGFAULT:
		// yield until the syscall/fault is handled
		runtime.Procyield()
	case INT_DISK:
		runtime.Proccontinue()
	case INT_KBD:
		runtime.Proccontinue()
	default:
		runtime.Pnum(trapno)
		runtime.Pnum(tf[TF_RIP])
		runtime.Pnum(0xbadbabe)
		for {}
	}
}

func trap_kbd(ts *trapstore_t) {
	fmt.Println("KEYBOARRRRRRRRRRRRD!!!!")
	cons.kbd_int <- true
}

type cons_t struct {
	kbd_int chan bool
	reader	chan []byte
	reqc	chan int
}

var cons	= cons_t{}

func kbd_daemon(cons *cons_t, km map[int]byte) {
	inb := runtime.Inb
	kready := func() bool {
		ibf := 1 << 0
		st := inb(0x64)
		if st & ibf == 0 {
			//panic("no kbd data?")
			return false
		}
		return true
	}
	var reqc chan int
	data := make([]byte, 0)
	for {
		select {
		case <- cons.kbd_int:
			if !kready() {
				continue
			}
			sc := inb(0x60)
			c, ok := km[sc]
			if ok {
				data = append(data, c)
			}
		case l := <- reqc:
			if l > len(data) {
				l = len(data)
			}
			data = data[0:l]
			cons.reader <- data
			data = make([]byte, 0)
		}
		if len(data) == 0 {
			reqc = nil
		} else {
			reqc = cons.reqc
		}
	}
}

// reads keyboard data, blocking for at least 1 byte. returns at most cnt
// bytes.
func kbd_get(cnt int) []byte {
	if cnt < 0 {
		panic("negative cnt")
	}
	cons.reqc <- cnt
	return <- cons.reader
}


func trap(handlers map[int]func(*trapstore_t)) {
	for {
		for cpu := 0; cpu < numcpus; cpu += 1 {

			head := cpus[cpu].tshead
			tail := cpus[cpu].tstail

			if tail == head {
				// no work for this cpu
				continue
			}

			tcur := trapstore_t{}
			tcur = cpus[cpu].trapstore[tail]

			trapno := tcur.trapno
			pid := tcur.pid

			tail = tsnext(tail)
			cpus[cpu].tstail = tail

			if h, ok := handlers[trapno]; ok {
				go h(&tcur)
				continue
			}
			panic(fmt.Sprintf("no handler for trap %v, pid %x\n",
			    trapno,pid))
		}
		runtime.Gosched()
	}
}

func trap_disk(ts *trapstore_t) {
	ide_int_done <- true
}

//var klock	= sync.Mutex{}

func kbd_init() {
	km := make(map[int]byte)
	NO := byte(0)
	tm := []byte{
	    // ty xv6
	    NO,   0x1B, '1',  '2',  '3',  '4',  '5',  '6',  // 0x00
	    '7',  '8',  '9',  '0',  '-',  '=',  '\b', '\t',
	    'q',  'w',  'e',  'r',  't',  'y',  'u',  'i',  // 0x10
	    'o',  'p',  '[',  ']',  '\n', NO,   'a',  's',
	    'd',  'f',  'g',  'h',  'j',  'k',  'l',  ';',  // 0x20
	    '\'', '`',  NO,   '\\', 'z',  'x',  'c',  'v',
	    'b',  'n',  'm',  ',',  '.',  '/',  NO,   '*',  // 0x30
	    NO,   ' ',  NO,   NO,   NO,   NO,   NO,   NO,
	    NO,   NO,   NO,   NO,   NO,   NO,   NO,   '7',  // 0x40
	    '8',  '9',  '-',  '4',  '5',  '6',  '+',  '1',
	    '2',  '3',  '0',  '.',  NO,   NO,   NO,   NO,   // 0x50
	    }

	for i, c := range tm {
		km[i] = c
	}
	cons.kbd_int = make(chan bool)
	cons.reader = make(chan []byte)
	cons.reqc = make(chan int)
	go kbd_daemon(&cons, km)
	irq_unmask(IRQ_KBD)
	// make sure kbd int is clear
	runtime.Inb(0x60)
}
func trap_syscall(ts *trapstore_t) {
	//klock.Lock()
	//defer klock.Unlock()

	pid  := ts.pid
	syscall(pid, &ts.tf)
}

func trap_pgfault(ts *trapstore_t) {

	pid := ts.pid
	fa  := ts.faultaddr
	proc := proc_get(pid)
	pte := pmap_walk(proc.pmap, fa, false, 0, nil)
	if pte != nil && *pte & PTE_COW != 0 {
		if fa < USERMIN {
			panic("kernel page marked COW")
		}
		sys_pgfault(proc, pte, fa, &ts.tf)
		return
	}

	rip := ts.tf[TF_RIP]
	fmt.Printf("*** fault *** %v: addr %x, rip %x. killing...\n",
	    proc.Name(), fa, rip)
	proc_kill(pid)
}

func tfdump(tf *[TFSIZE]int) {
	fmt.Printf("RIP: %#x\n", tf[TF_RIP])
	fmt.Printf("RAX: %#x\n", tf[TF_RAX])
	fmt.Printf("RDI: %#x\n", tf[TF_RDI])
	fmt.Printf("RSI: %#x\n", tf[TF_RSI])
	fmt.Printf("RBX: %#x\n", tf[TF_RBX])
	fmt.Printf("RCX: %#x\n", tf[TF_RCX])
	fmt.Printf("RDX: %#x\n", tf[TF_RDX])
	fmt.Printf("RSP: %#x\n", tf[TF_RSP])
}

// XXX
func cdelay(n int) {
	for i := 0; i < n*1000000; i++ {
	}
}

type fd_t struct {
	file	*file_t
	offset	int
	perms	int
}

var dummyfile	= file_t{-1}

// special fds
var fd_stdin 	= fd_t{&dummyfile, 0, 0}
var fd_stdout 	= fd_t{&dummyfile, 0, 0}
var fd_stderr 	= fd_t{&dummyfile, 0, 0}

type proc_t struct {
	pid	int
	name	string
	// all pages
	maplock	sync.Mutex
	pages	map[int]*[512]int
	// physical -> user va mapping
	upages	map[int]int
	pmap	*[512]int
	p_pmap	int
	dead	bool
	fds	map[int]*fd_t
	nextfd	int
	cwd	string
}

func (p *proc_t) Name() string {
	return "\"" + p.name + "\""
}

var proclock = sync.Mutex{}
var allprocs = map[int]*proc_t{}

var pid_cur  int
func proc_new(name string) *proc_t {
	ret := &proc_t{}

	proclock.Lock()
	pid_cur++
	newpid := pid_cur
	allprocs[newpid] = ret
	proclock.Unlock()

	ret.name = name
	ret.pid = newpid
	ret.pages = make(map[int]*[512]int)
	ret.upages = make(map[int]int)
	ret.fds = map[int]*fd_t{0: &fd_stdin, 1: &fd_stdout, 2: &fd_stderr}
	ret.nextfd = len(ret.fds)
	ret.cwd = "/"

	return ret
}

func proc_get(pid int) *proc_t {
	proclock.Lock()
	p, ok := allprocs[pid]
	proclock.Unlock()
	if !ok {
		panic(fmt.Sprintf("no such pid %d", pid))
	}
	return p
}

func (p *proc_t) fd_new() (int, *fd_t) {
	fdn := p.nextfd
	p.nextfd++
	fd := &fd_t{}
	if _, ok := p.fds[fdn]; ok {
		panic(fmt.Sprintf("new fd exists %d", fdn))
	}
	p.fds[fdn] = fd
	return fdn, fd
}

func (p *proc_t) page_insert(va int, pg *[512]int, p_pg int,
    perms int, vempty bool) {

	pte := pmap_walk(p.pmap, va, true, perms, p.pages)
	ninval := false
	if pte != nil && *pte & PTE_P != 0 {
		if vempty {
			panic("pte not empty")
		}
		ninval = true
		// remove from tracking maps
		p_rem := *pte & PTE_ADDR
		if _, ok := p.pages[p_rem]; !ok {
			panic("kern va not tracked")
		}
		if _, ok := p.upages[p_rem]; !ok {
			panic("user va not tracked")
		}
		delete(p.pages, p_rem)
		delete(p.upages, p_rem)
	}
	*pte = p_pg | perms | PTE_P
	if ninval {
		invlpg(va)
	}

	p.pages[p_pg] = pg
	p.upages[p_pg] = va & PGMASK
}

func (p *proc_t) page_remove(va int, pg *[512]int) {

	pte := pmap_walk(p.pmap, va, false, 0, nil)
	if pte != nil && *pte & PTE_P != 0 {
		p_pa := *pte & PTE_ADDR
		delete(p.pages, p_pa)
		delete(p.upages, p_pa)
		*pte = 0
		invlpg(va)
	}
}

func (p *proc_t) sched_add(tf *[TFSIZE]int) {
	runtime.Procadd(tf, p.pid, p.p_pmap)
}

func proc_kill(pid int) {
	proclock.Lock()
	p, ok := allprocs[pid]
	if !ok {
		panic("bad pid")
	}
	p.dead = true
	delete(allprocs, pid)
	proclock.Unlock()

	runtime.Prockill(pid)
	// XXX
	//fmt.Printf("not cleaning up\n")
	//return

	//ms := runtime.MemStats{}
	//runtime.ReadMemStats(&ms)
	//before := ms.Alloc

	runtime.GC()
	//runtime.ReadMemStats(&ms)
	//after := ms.Alloc
	//fmt.Printf("reclaimed %vK\n", (before-after)/1024)
}

func mp_sum(d []uint8) int {
	ret := 0
	for _, c := range d {
		ret += int(c)
	}
	return ret & 0xff
}

func mp_tblget(f []uint8) ([]uint8, []uint8, []uint8) {
	feat := readn(f, 1, 11)
	if feat != 0 {
		panic("\"default\" configurations not supported")
	}
	ctbla := readn(f, 4, 4)
	c := dmap8(ctbla)
	sig := readn(c, 4, 0)
	// sig is "PCMP"
	if sig != 0x504d4350 {
		panic("bad conf table sig")
	}

	bsz := readn(c, 2, 4)
	base := c[:bsz]
	if mp_sum(base) != 0 {
		panic("bad conf table cksum")
	}

	esz := readn(c, 2, 0x28)
	eck := readn(c, 1, 0x2a)
	extended := c[bsz : bsz + esz]

	esum := (mp_sum(extended) + eck) & 0xff
	if esum != 0 {
		panic("bad extended table checksum")
	}

	return f, base, extended
}

func mp_scan() ([]uint8, []uint8, []uint8) {
	assert_no_phys(kpmap(), 0)

	// should use ACPI. don't bother to scan other areas as recommended by
	// MP spec.
	// try bios readonly memory, from 0xe0000-0xfffff
	bro := 0xe0000
	const brolen = 0x1ffff
	p := (*[brolen]uint8)(unsafe.Pointer(dmap(bro)))
	for i := 0; i < brolen - 4; i++ {
		if p[i] == '_' &&
		   p[i+1] == 'M' &&
		   p[i+2] == 'P' &&
		   p[i+3] == '_' && mp_sum(p[i:i+16]) == 0 {
			return mp_tblget(p[i:i+16])
		}
	}
	return nil, nil, nil
}

type mpcpu_t struct {
	lapid    int
	bsp      bool
}

func cpus_find() []mpcpu_t {

	fl, base, _ := mp_scan()
	if fl == nil {
		fmt.Println("uniprocessor")
		return nil
	}

	ret := make([]mpcpu_t, 0, 10)

	// runtime does the same check
	lapaddr := readn(base, 4, 0x24)
	if lapaddr != 0xfee00000 {
		panic(fmt.Sprintf("weird lapic addr %#x", lapaddr))
	}

	// switch to symmetric mode interrupts if we are in PIC mode
	mpfeat := readn(fl, 4, 12)
	imcrp := 1 << 7
	if mpfeat & imcrp != 0 {
		fmt.Printf("entering symmetric mode\n")
		// select imcr
		runtime.Outb(0x22, 0x70)
		// "writing a value of 01h forces the NMI and 8259 INTR signals
		// to pass through the APIC."
		runtime.Outb(0x23, 0x1)
	}

	ecount := readn(base, 2, 0x22)
	entries := base[44:]
	idx := 0
	for i := 0; i < ecount; i++ {
		t := readn(entries, 1, idx)
		switch t {
		case 0:		// processor
			idx += 20
			lapid  := readn(entries, 1, idx + 1)
			cpuf   := readn(entries, 1, idx + 3)
			cpu_en := 1
			cpu_bp := 2

			bsp := false
			if cpuf & cpu_bp != 0 {
				bsp = true
			}

			if cpuf & cpu_en == 0 {
				fmt.Printf("cpu %v disabled\n", lapid)
				continue
			}

			ret = append(ret, mpcpu_t{lapid, bsp})

		default:	// bus, IO apic, IO int. assignment, local int
				// assignment.
			idx += 8
		}
	}
	return ret
}

func cpus_stack_init(apcnt int, stackstart int) {
	for i := 0; i < apcnt; i++ {
		kmalloc(stackstart, PTE_W)
		stackstart += PGSIZE
		assert_no_va_map(kpmap(), stackstart)
		stackstart += PGSIZE
	}
}

func cpus_start() {
	cpus := cpus_find()
	apcnt := len(cpus) - 1
	fmt.Printf("found %v CPUs\n", len(cpus))

	if apcnt == 0 {
		fmt.Printf("uniprocessor with mpconf\n")
	}

	// the top of bsp's interrupt stack is 0xa100001000. map an interrupt
	// stack for each ap. leave 0xa100001000 as a guard page.
	stackstart := 0xa100002000
	cpus_stack_init(apcnt, stackstart)

	// AP code must be between 0-1MB because the APs are in real mode. load
	// code to 0x8000 (overwriting bootloader)
	mpaddr := 0x8000
	mppg := dmap(mpaddr)
	for i := range mppg {
		mppg[i] = 0
	}
	mpcode := allbins["mpentry.bin"].data
	runtime.Memmove(unsafe.Pointer(mppg), unsafe.Pointer(&mpcode[0]),
	    len(mpcode))

	// skip mucking with CMOS reset code/warm reset vector (as per the the
	// "universal startup algoirthm") and instead use the STARTUP IPI which
	// is supported by lapics of version >= 1.x. (the runtime panicks if a
	// lapic whose version is < 1.x is found, thus assume their absence).
	// however, only one STARTUP IPI is accepted after a CPUs RESET or INIT
	// pin is asserted, thus we need to send an INIT IPI assert first (it
	// appears someone already used a STARTUP IPI; probably the BIOS).

	lapaddr := 0xfee00000
	pte := pmap_walk(kpmap(), lapaddr, false, 0, nil)
	if pte == nil || *pte & PTE_P == 0 || *pte & PTE_PCD == 0 {
		panic("lapaddr unmapped")
	}
	lap := (*[PGSIZE/4]uint32)(unsafe.Pointer(uintptr(lapaddr)))
	icrh := 0x310/4
	icrl := 0x300/4

	ipilow := func(ds int, t int, l int, deliv int, vec int) uint32 {
		return uint32(ds << 18 | t << 15 | l << 14 |
		    deliv << 8 | vec)
	}

	icrw := func(hi uint32, low uint32) {
		// use sync to guarantee order
		atomic.StoreUint32(&lap[icrh], hi)
		atomic.StoreUint32(&lap[icrl], low)
	}

	// destination shorthands:
	// 1: self
	// 2: all
	// 3: all but me

	initipi := func(assert bool) {
		vec := 0
		delivmode := 0x5
		level := 1
		trig  := 0
		dshort:= 3
		if !assert {
			trig = 1
			level = 0
			dshort = 2
		}
		hi  := uint32(0)
		low := ipilow(dshort, trig, level, delivmode, vec)
		icrw(hi, low)
	}

	startupipi := func() {
		delivmode := 0x6
		level     := 0x1
		dshort    := 0x3
		trig      := 0x0
		vec       := mpaddr >> 12

		hi := uint32(0)
		low := ipilow(dshort, trig, level, delivmode, vec)
		icrw(hi, low)
	}

	// pass arguments to the ap startup code via secret storage (the old
	// boot device page at 0x7c00)

	// secret storage layout
	// 0 - e820map
	// 1 - pmap
	// 2 - firstfree
	// 3 - ap entry
	// 4 - gdt
	// 5 - gdt
	// 6 - idt
	// 7 - idt
	// 8 - ap count
	// 9 - stack start

	ss := (*[10]int)(unsafe.Pointer(uintptr(0x7c00)))
	sap_entry := 3
	sgdt      := 4
	sidt      := 6
	sapcnt    := 8
	sstacks   := 9
	ss[sap_entry] = runtime.Fnaddri(ap_entry)
	// sgdt and sidt save 10 bytes
	runtime.Sgdt(&ss[sgdt])
	runtime.Sidt(&ss[sidt])
	ss[sapcnt] = 0
	ss[sstacks] = stackstart   // each ap grabs a unique stack

	initipi(true)
	// not necessary since we assume lapic version >= 1.x (ie not 82489DX)
	//initipi(false)
	cdelay(1)

	startupipi()
	cdelay(1)
	startupipi()

	// wait for APs to become ready
	for ss[sapcnt] != apcnt {
	}
	numcpus = apcnt + 1

	fmt.Printf("done! %v CPUs joined\n", ss[sapcnt])
}

// myid is a logical id, not lapic id
//go:nosplit
func ap_entry(myid int) {

	// myid starts from 1
	runtime.Ap_setup(myid)

	lid := lap_id()
	if lid > maxcpus || lid < 0 {
		runtime.Pnum(0xb1dd1e)
		for {
		}
	}
	cpus[lid].num = myid

	// ints are still cleared
	runtime.Procyield()
}

func init_8259() {
	// the piix3 provides two 8259 compatible pics. the runtime masks all
	// irqs for us.
	outb := func(reg int, val int) {
		runtime.Outb(int32(reg), int32(val))
		cdelay(1)
	}
	pic1 := 0x20
	pic1d := pic1 + 1
	pic2 := 0xa0
	pic2d := pic2 + 1

	runtime.Cli()

	// master pic
	// start icw1: icw4 required
	outb(pic1, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	outb(pic1d, IRQ_BASE)
	// icw3, cascaded mode
	outb(pic1d, 4)
	// icw4, auto eoi, intel arch mode.
	outb(pic1d, 3)

	// slave pic
	// start icw1: icw4 required
	outb(pic2, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	outb(pic2d, IRQ_BASE + 8)
	// icw3, slave identification code
	outb(pic2d, 2)
	// icw4, auto eoi, intel arch mode.
	outb(pic2d, 3)

	// ocw3, enable "special mask mode" (??)
	outb(pic1, 0x68)
	// ocw3, read irq register
	outb(pic1, 0x0a)

	// ocw3, enable "special mask mode" (??)
	outb(pic2, 0x68)
	// ocw3, read irq register
	outb(pic2, 0x0a)

	// enable slave 8259
	irq_unmask(2)
	// all irqs go to CPU 0 until redirected

	runtime.Sti()
}

var intmask uint16 	= 0xffff

func irq_unmask(irq int) {
	if irq < 0 || irq > 16 {
		panic("weird irq")
	}
	pic1 := int32(0x20)
	pic1d := pic1 + 1
	pic2 := int32(0xa0)
	pic2d := pic2 + 1
	intmask = intmask & ^(1 << uint(irq))
	dur := int32(intmask)
	runtime.Outb(pic1d, dur)
	runtime.Outb(pic2d, dur >> 8)
}

func main() {
	// magic loop
	//if rand.Int() != 0 {
	//	for {
	//	}
	//}

	dmap_init()
	runtime.Install_traphandler(trapstub)

	trap_diex := func(c int) func(*trapstore_t) {
		return func(ts *trapstore_t) {
			fmt.Printf("[death on trap %v]\n", c)
			panic("perished")
		}
	}

	handlers := map[int]func(*trapstore_t) {
	     GPFAULT: trap_diex(GPFAULT),
	     PGFAULT: trap_pgfault,
	     SYSCALL: trap_syscall,
	     INT_DISK: trap_disk,
	     INT_KBD: trap_kbd,
	     }
	go trap(handlers)

	init_8259()
	//cpus_start()
	kbd_init()
	if !ide_init() {
		panic("no IDE disk")
	}
	fs_init()
	fmt.Printf("morimolymoly was here!\n")
	exec := func(cmd string) {
		path := strings.Split(cmd, "/")
		ret := sys_execv(path, nil)
		if ret != 0 {
			panic(fmt.Sprintf("exec failed %v", ret))
		}
	}

	//exec("bin/fault")
	//exec("bin/hello")
	//exec("bin/fork")
	//exec("bin/fstest")
	//exec("bin/fslink")
	exec("bin/fsunlink")
	//exec("bin/fswrite")
	//exec("bin/fsbigwrite")
	//exec("bin/fsmkdir")
	//exec("bin/fscreat")
	//exec("bin/getpid")

	//ide_test()
	//bc_test()
	//sb_test()
	//balloc_test()
	//fs_fmt()
	//lsonly()

	//dur := make(chan bool)
	//<- dur
	fake_work()
}

func ide_test() {
	req := idereq_new(0, false, nil)
	ide_request <- req
	<- req.ack
	fmt.Printf("read of block %v finished!\n", req.buf.block)
	for _, c := range req.buf.data {
		fmt.Printf("%x ", c)
	}
	fmt.Printf("\n")
	fmt.Printf("will overwrite block %v now...\n", req.buf.block)
	req.write = true
	for i := range req.buf.data {
		req.buf.data[i] = 0xcc
	}
	ide_request <- req
	<- req.ack
	fmt.Printf("done!\n")
}

//func lsonly() {
//	rootinode, rootioff := superb.rootinode()
//	fmt.Printf("lising root dir...\n")
//	ls(rootinode, rootioff)
//}

//func file_pr(fn string, fibn int, fioff int) bool {
//	if fn == "" {
//		return false
//	}
//	finode := inode_get(fibn, fioff, false, 0)
//	sz := finode.size()
//	if finode.itype() == I_DIR {
//		fmt.Printf("drw-r--r-- %10d %s/\n", sz, fn)
//		return true
//	}
//
//	fmt.Printf("-rw-r--r-- %10d %s\n", sz, fn)
//	return false
//}

//func ls(dirnode int, ioff int) {
//	ip := inode_get(dirnode, ioff, true, I_DIR)
//	if ip.itype() != I_DIR {
//		panic("this is not a directory")
//	}
//	recdirn := make([]string, 0)
//	recenc := make([]int, 0)
//	for i := 0; i < ip.size()/512; i++ {
//		if i > NIADDRS {
//			// use indirect block
//			panic("no imp")
//		}
//		dblk := bread(ip.addr(i))
//		// dump all files listed in this dir data block
//		for j := 0; j < NDIRENTS; j++ {
//			de := dirdata_t{dblk}
//			din := int(de.inodenext(j))
//			dib, dioff := bidecode(din)
//			fn := de.filename(j)
//			isdir := file_pr(fn, dib, dioff)
//			if isdir {
//				recdirn = append(recdirn, fn)
//				recenc = append(recenc, din)
//			}
//		}
//		brelse(dblk)
//	}
//
//	for i, encd := range recenc {
//		fmt.Printf("\t%s/\n", recdirn[i])
//		dn, ii := bidecode(encd)
//		ls(dn, ii)
//	}
//}

func fake_work() {
	fmt.Printf("'network' test\n")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)
}

type packet struct {
	ipaddr 		int
	payload		int
}

func genpackets(ch chan packet) {
	for {
		n := packet{rand.Intn(1000), 0}
		ch <- n
	}
}

func spawnsend(p packet, outbound map[int]chan packet, f func(chan packet)) {
	pri := p_priority(p)
	if ch, ok := outbound[pri]; ok {
		ch <- p
	} else {
		pch := make(chan packet)
		outbound[pri] = pch
		go f(pch)
		pch <- p
	}
}

func p_priority(p packet) int {
	return p.ipaddr / 100
}

func process_packets(in chan packet) {
	outbound := make(map[int]chan packet)

	for {
		p := <- in
		spawnsend(p, outbound, ip_process)
	}
}

func ip_process(ipchan chan packet) {
	for {
		p := <- ipchan
		if rand.Intn(1000) == 1 {
			fmt.Printf("%v ", p_priority(p))
		}
	}
}
