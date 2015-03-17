# Go Checkers

A network server for the checkers system written in the Go programming language.

## Why?

I wrote a single-threaded server in Python using non-blocking IO and select. I had been meaning to learn Go and I thought that re-implementing the same server would allow me to compare the result, both stylistically and from a performance perspective.

## Requirements

To run this program, you'll need:

  * A working Go environment

## Installing

To install the server, simply run the following:

```
go install github.com/batkinson/checkers-go/checkers-server
```

## Running

After installing the server, you should be able to run it:

```
checkers-server
```
