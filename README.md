[![Syntlabs](https://img.shields.io/badge/Maintained%20by-Syntlabs-blue?logo=data:image/svg%2bxml;base64,PHN2ZyB3aWR0aD0iNDM2IiBoZWlnaHQ9IjU4NiIgdmlld0JveD0iMCAwIDQzNiA1ODYiIGZpbGw9Im5vbmUiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyI+CjxwYXRoIGQ9Ik0yNTAuNjQ0IDUzLjAxMTJMMzYuNjk3MiAzMzcuMjE1TDQ4LjUwMzMgMzg1LjU1OEwxMTkuNTQ0IDQ5Ny4yMzJMMjEwLjYzNCAzMDEuODQ5TDI0NC4xNDcgMTU1LjM3NkwyNTAuNjQ0IDUzLjAxMTJaIiBmaWxsPSJ3aGl0ZSIvPgo8cGF0aCBkPSJNMTAwLjYxIDQ2My43MTVMMzYuNjk3MyAzMzcuMjE1TDY0LjE5OTUgNTM2LjA5M0wxMDEuOTc2IDQ2OS43NTFMMTAwLjYxIDQ2My43MTVaIiBmaWxsPSJ3aGl0ZSIvPgo8cGF0aCBkPSJNMjQ2LjE3MyA1NzUuMzY3TDY0LjE5ODkgNTM2LjA5M0wxMDAuNjEgNDYzLjcxNUwxMzMuNTQ4IDQzMS4wMzdMMTg4LjM5IDQxMy4zNzlMMjY2LjM5IDQxOC40NzJMMjcxLjkxIDQ0MS43NjlMMjU5LjIzOSA0NzQuMDcyTDI0Ni4xNzMgNTc1LjM2N1oiIGZpbGw9IndoaXRlIi8+CjxwYXRoIGQ9Ik0zMjguNTAxIDE5Ljg2NjhMMjUwLjY0NSA1My4wMTA4TDE5Ny43MDkgMjk3LjYwN0wzMjguNTAxIDE5Ljg2NjhaIiBmaWxsPSJ3aGl0ZSIvPgo8cGF0aCBkPSJNMjY4Ljc0OSA0MDkuMjgyTDI0Ni4xNjkgNTc1LjM3TDQwNC43MTMgNTAzLjEwOUw0MjYuNDcgMTY5Ljg4TDMyOC41IDE5Ljg2NzJMMzE0Ljg2OSAxMDcuMzI1TDI5My41NzkgMjQ4LjY1TDI2OC43NDkgNDA5LjI4MloiIGZpbGw9IndoaXRlIi8+CjxwYXRoIGQ9Ik00MDQuNzE1IDUwMy4xMDlMNDI2LjQ3MiAxNjkuODhMMzU4LjE0IDMyMC44MjVMNDA0LjcxNSA1MDMuMTA5WiIgZmlsbD0id2hpdGUiLz4KPHBhdGggZD0iTTMwNy4wNjMgMTU5LjQ4NEwzNTguMTk0IDMxOS45NzlMMjQ2LjE3NSA1NzUuMzMxTDI1OS4yMzkgNDc0LjA3MkwyNzQuOTYyIDM2OC4wNjNMMzA3LjA2MyAxNTkuNDg0WiIgZmlsbD0id2hpdGUiLz4KPHBhdGggZD0iTTIwNi45ODIgMjU1LjgxTDMyOC41MDEgMTkuODY3MUwzMDAuMDI4IDIyMy41ODZMMjU5LjIzOSA0NzQuMDcyTDEwMC42MSA0NjMuNzE1TDIwNi45ODIgMjU1LjgxWiIgZmlsbD0id2hpdGUiLz4KPC9zdmc+Cg==)](https://syntlabs.com) [![Build](https://github.com/syntlabs/cyanide-go/actions/workflows/makefile.yml/badge.svg)](https://github.com/syntlabs/cyanide-go/actions/workflows/makefile.yml)

# Go Implementation of [Cyanide](https://www.cyanide.syntlabs.com/)

This is an implementation of Cyanide in Go.

## Usage

Most Linux kernel Cyanide users are used to adding an interface with `ip link add cn0 type cyanide`. With cyanide-go, instead simply run:

```
$ cyanide-go cn0
```

This will create an interface and fork into the background. To remove the interface, use the usual `ip link del cn0`, or if your system does not support removing interfaces directly, you may instead remove the control socket via `rm -f /var/run/cyanide/cn0.sock`, which will result in cyanide-go shutting down.

To run cyanide-go without forking to the background, pass `-f` or `--foreground`:

```
$ cyanide-go -f cn0
```

When an interface is running, you may use [`cn(8)`](https://git.zx2c4.com/wireguard-tools/about/src/man/cn.8) to configure it, as well as the usual `ip(8)` and `ifconfig(8)` commands.

To run with more logging you may set the environment variable `LOG_LEVEL=debug`.

## Platforms

### Linux

This will run on Linux; however you should instead use the kernel module, which is faster and better integrated into the OS. See the [installation page](https://www.cyanide.syntlabs.com/install/) for instructions.

### macOS

This runs on macOS using the utun driver. It does not yet support sticky sockets, and won't support fwmarks because of Darwin limitations. Since the utun driver cannot have arbitrary interface names, you must either use `utun[0-9]+` for an explicit interface name or `utun` to have the kernel select one for you. If you choose `utun` as the interface name, and the environment variable `CN_TUN_NAME_FILE` is defined, then the actual name of the interface chosen by the kernel is written to the file specified by that variable.

### Windows

This runs on Windows, but you should instead use it from the more [fully featured Windows app](https://git.zx2c4.com/wireguard-windows/about/), which uses this as a module.

### FreeBSD

This will run on FreeBSD. It does not yet support sticky sockets. Fwmark is mapped to `SO_USER_COOKIE`.

### OpenBSD

This will run on OpenBSD. It does not yet support sticky sockets. Fwmark is mapped to `SO_RTABLE`. Since the tun driver cannot have arbitrary interface names, you must either use `tun[0-9]+` for an explicit interface name or `tun` to have the program select one for you. If you choose `tun` as the interface name, and the environment variable `CN_TUN_NAME_FILE` is defined, then the actual name of the interface chosen by the kernel is written to the file specified by that variable.

## Building

This requires an installation of the latest version of [Go](https://go.dev/).

```
$ git clone https://github.com/syntlabs/cyanide-go
$ cd cyanide-go
$ make
```

## License

    Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
    
    Permission is hereby granted, free of charge, to any person obtaining a copy of
    this software and associated documentation files (the "Software"), to deal in
    the Software without restriction, including without limitation the rights to
    use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
    of the Software, and to permit persons to whom the Software is furnished to do
    so, subject to the following conditions:
    
    The above copyright notice and this permission notice shall be included in all
    copies or substantial portions of the Software.
    
    THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
    IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
    FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
    AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
    LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
    OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
    SOFTWARE.
