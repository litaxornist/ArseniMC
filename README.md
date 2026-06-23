# ArseniMC

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg?style=for-the-badge&logo=go)](https://golang.org/)
[![Platform](https://img.shields.io/badge/Platform-macOS_ARM64-black.svg?style=for-the-badge&logo=apple)](https://apple.com/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](#license)

> A bare-metal, native ARM64 execution environment and server lifecycle manager for Minecraft on Apple Silicon.

## Overview

ArseniMC replaces traditional Java-wrapper launchers with a monolithic, statically compiled Go binary. It handles Microsoft OAuth2 device-code authentication, concurrent asset resolution, and headless server generation natively on macOS, bypassing standard client bottlenecks.

Support for `x86_64` (Intel) architecture is intentionally omitted to maintain strict optimization for the `darwin/arm64` execution environment.

## Features

* **Native Cryptographic Auth:** Implements the complete Microsoft to XSTS to Minecraft OAuth2 token exchange via terminal device codes.
* **Concurrent Resolution:** Resolves and fetches Mojang, Paper, and Fabric meta-manifests and classpaths concurrently using Go routines.
* **Headless Injection:** Executes GUI-based modloader installers (Fabric, Forge, NeoForge) silently in the background, injecting patched profiles into the target directory.
* **Hardware Telemetry:** Detaches server instances to `tmux` multiplexers, piping raw Darwin kernel metrics (`top`, `iostat`, `netstat`) directly to `stdout`.

## Prerequisites

Ensure the host machine meets the following environmental requirements before compilation or execution:

* Apple Silicon Mac (M1/M2/M3/M4)
* macOS 12.0+ 
* `go` (>= 1.22)
* `openjdk@21`
* `tmux`

## Building

The recommended installation path is via Homebrew, which compiles the binary directly against the host architecture.

```sh
brew tap litaxornist/arsenimc
brew install arsenimc
```

To build manually from source:

```sh
git clone [https://github.com/](https://github.com/)litaxornist/ArseniMC.git
cd ArseniMC
go mod tidy
go build -ldflags="-s -w" -o arsenimc main.go
sudo mv arsenimc /usr/local/bin/
```

## Usage

ArseniMC is strictly operated via command-line arguments. 

### Authentication
Initialize the OAuth2 device-code handshake. Tokens are stored in `~/.auth`.
```sh
arsenimc a
```
*Provide a pre-authorized token directly using `-k <token>`.*

### Client Initialization
Construct a client environment, resolving all natives, libraries, and modloader dependencies.
```sh
arsenimc -d <version> <target_type> <instance_name>
```
*Example:* `arsenimc -d 1.21.1 fabric client_01`

### Server Initialization
Generate a headless server environment. Auto-accepts EULA and writes networking configuration.
```sh
arsenimc -s <version> <target_type> <instance_name> [port]
```
*Example:* `arsenimc -s 1.21.1 paper node_01 25565`

### Execution & Telemetry
Fire the Java payload. Targetting a server (`-s`) will drop into a live hardware telemetry monitor.
```sh
arsenimc start -c <instance_name>
arsenimc start -s <instance_name>
```

### Dependency Injection
Drop `.jar` dependencies directly into target environments.
```sh
arsenimc -m <direct_url> <instance_name>
```

## Disclaimer

ArseniMC is an independent open-source utility. It is not affiliated with, endorsed by, or connected to Mojang AB, Microsoft Corporation, or Apple Inc. All network requests are made directly to public APIs provided by Mojang, PaperMC, and FabricMC.

## License

This software is licensed under the MIT License. See the `LICENSE` file for full text.
