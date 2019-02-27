TOP := .

BASEFLAGS := -Wall -Werror -MMD -MP -O -g
ASFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP) -m32
CFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP) -fno-stack-protector -fno-builtin

# bootloader sources
ASMS := boot.S
CS   := ./bootloader/bootmain.c ./util/chentry.c
SRCS := $(ASMS) $(CS)

# kernel sources
KSRC := main.go syscall.go pmap.go fs.go

OBJS := $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS)))

UBINS := hello fault fork getpid fstest fswrite fsmkdir fscreat fsbigwrite \
	  fslink fsunlink
FSUPROGS := $(patsubst %,fsdir/bin/%,$(UBINS))
UPROGS := $(patsubst %,user/%,$(UBINS))

BGOS := mpentry.bin.bgo

RFS  := $(patsubst %.c,%.d,$(CS))
RFS  += $(patsubst %,%.d,$(UPROGS))
RFS  += user/litc.d

GOBIN := ../bin/go
SKEL := fsdir
SKELDEPS := $(shell find $(SKEL))

CPUS := $(shell echo $${CPUS:-1})
QOPTS := -m 256M -smp cpus=$(CPUS)

OS := $(shell uname -s)

QEMU := qemu-system-x86_64

ifeq ($(OS), OpenBSD)
	ASFLAGS += -nopie
endif

all: go.img

-include $(RFS)

boot: boot.o bootmain.o
	$(LD) --omagic -o $@.elf -m elf_i386 -static -e start -Ttext 0x7c00 $^
	objcopy -O binary $@.elf $@.bin
	./stamp.py $@.bin
	mv $@.bin $@

bootmain.o: ./bootloader/bootmain.c
	@# don't use -Os because it rearranges code in .text such that start
	@# isn't first. htf do i force ld to put a specific function first in
	@# the section?
	@#$(CC) -c $(CFLAGS) -Os $<
	$(CC) -m32 -c -Ibootloader $(CFLAGS) $<


go.img: boot main.gobin $(SKELDEPS) $(FSUPROGS)
	./mkbdisk.py boot main.gobin $@ $(SKEL) || { rm -f $@; false; }

# the user/% prereq is built by the UPROGS target
$(FSUPROGS): fsdir/bin/% : user/%
	objcopy -S $^ $@

$(GOBIN):
	@echo; echo; echo; \
		echo 'You need to build go:'; \
		echo 'cd ../src && ./all.bash'; \
		echo; echo; echo; \
		false

main.gobin: chentry $(GOBIN) bins.go $(KSRC)
	$(GOBIN) build -o $@ bins.go $(KSRC)
	ADDR=0x`nm main.gobin |grep _rt0_hack |cut -f1 -d' '`; \
		if test "$$ADDR" = "0x"; then echo no _rt0_hack; false; \
		else ./chentry $@ $$ADDR; fi \

$(UPROGS): CFLAGS += -I user/ -fPIC
$(UPROGS): % : %.c user/litc.o
	$(CC) $(CFLAGS) -I user/ -Wl,-T user/linker.ld \
	    -Wl,-n -o $@ $< user/litc.o

mpentry.bin: mpentry.S
	$(CC) -o mpentry.elf -Ttext 0x8000 -Wl,-n -no-pie -nostdlib $^
	objcopy -O binary -j.text mpentry.elf $@

$(BGOS): %.bgo : %
	./bin2go.sh $< > $@

bins.go: $(BGOS) prepbins.py
	./prepbins.py $(BGOS) > $@


chentry: ./util/chentry.c
	$(CC) $(BASEFLAGS) -o $@ ./util/chentry.c

clean:
	rm -f $(BGOS) $(OBJS) $(RFS) boot.elf d.img main boot main.gobin \
	    go.img chentry mpentry.elf mpentry.bin bins.go user/litc.o \
	    $(FSUPROGS) $(UPROGS)

qemu: go.img
	$(QEMU) $(QOPTS) -hda go.img

.PHONY: clean qemu qemu-gdb gqemu gqemux gqemu-gdb gqemux-gdb
