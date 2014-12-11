package main

import (
   "io"
   "bufio"
   "net"
   "log"
	"fmt"
)

func main() {
   lnr, err := net.Listen("tcp", ":5000")
   if err != nil {
      log.Fatal(err)
   }
   for {
      conn, err := lnr.Accept()
      if err != nil {
         log.Println(err)
         continue
      }
      go handle(conn)
   }
}

func handle(c net.Conn) {
   defer func() {
      fmt.Println("closing ", c.RemoteAddr())
      c.Close()
   }()
   fmt.Printf("connect from %v\n", c.RemoteAddr())
   lines := bufio.NewReader(c)
   for {
      line, err := lines.ReadString('\n')
      io.WriteString(c, line)
      if err != nil {
         break
      }
   }
}

