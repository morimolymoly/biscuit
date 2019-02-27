# USAGE
```sh
cd src && export CC="gcc -Wimplicit-fallthrough=0 -Wno-error=shift-negative-value -Wno-shift-negative-value" && env CGO_ENABLED=0  ./make.bash && cd ../biscuit && make qemu
```
