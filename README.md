# Qsnap

This is a replacement for Osnap, a snapshot testing tool.

## Run development version

```bash
git clone https://github.com/maxischmaxi/qsnap.git
cd qsnap
go run cmd/qsnap/main.go -input /path/to/component-library/project -storybookForce true
```

## Installation

```bash
go install github.com/maxischmaxi/qsnap
```

## Usage

```bash
go run cmd/qsnap/main.go -input /path/to/component-library/project
qsnap -input /path/to/component-library/project
```

or, if you're currently inside the project directory:

```bash
qsnap
```
