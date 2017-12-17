package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
)

// PORT_DEFAULT는 기본으로 사용하는 포트 번호입니다.
const PORT_DEFAULT = 8080

// usage는 이 프록시의 사용법을 stdout에 출력하고 프로그램을 종료합니다.
func usage() {
	fmt.Println("Usage: site_unblock <PORT>")
	fmt.Println("[+] Default PORT is ", PORT_DEFAULT)
	os.Exit(-1)
}

// read_http_request는 bufio.Reader를 하나 받아, HTTP 패킷을 하나 읽어냅니다.
// 그 후, 읽어낸만큼의 []byte와 해당 HTTP 패킷의 HOST를 반환합니다.
// + 패킷 안에 Content-Length가 있어야 작동합니다.
// + 무엇인가 잘못되는 경우는 panic합니다.
func read_http_request(reader *bufio.Reader) ([]byte, string) {
	var host []byte = nil
	content_length := 0
	packet := make([]byte, 0, 64)

	// 1. 헤더 읽기
	for {
		// 각각의 헤더 한 줄은 "\r\n"으로 끝나므로 '\n'을 찾아줍니다.
		header_line, err := reader.ReadBytes('\n')
		if err != nil {
			// 헤더도 다 못 읽었는데 통신이 끝났으므로 잘못된 요청입니다.
			panic("Wrong http request(bad header)")
		}

		// 반환할 패킷에 읽은 줄을 추가합니다.
		packet = append(packet, header_line...)

		// 빈 줄이 나왔으므로 헤더가 끝났습니다.
		if len(header_line) == 2 {
			break
		}

		// Host
		if bytes.HasPrefix(header_line, []byte("Host: ")) {
			if host != nil {
				panic("Wrong http request(too many host)")
			}
			host = header_line[6 : len(header_line)-2]
		}

		// Content-length
		if bytes.HasPrefix(header_line, []byte("Content-Length: ")) {
			if content_length != 0 {
				panic("Wrong http request(too many content-length)")
			}
			content_length, err = strconv.Atoi(
				string(header_line[16 : len(header_line)-2]))
			if err != nil {
				panic("Wrong http request(bad content-length)")
			}
		}
	}

	// 추가적인 예외 처리를 해줍니다.
	if host == nil {
		panic("Wrong http request(no host)")
	}
	host_str := string(host)

	// Content-Length가 없다면 GET과 같은 헤더만 있는 요청이므로, 다 읽었기 때문에
	// 중단해도 됩니다.
	if content_length == 0 {
		return packet, host_str
	}

	// Content-Length만큼의 byte를 추가로 읽습니다.
	tmp := make([]byte, content_length)
	_, err := io.ReadFull(reader, tmp)
	if err != nil {
		panic("Wrong http request(short content)")
	}

	packet = append(packet, tmp...)
	return packet, host_str
}

// over_the_horizon은 http connection을 받아 HTTP 통신을 하게 합니다.
// 이 과정에서 warning.or.kr과 같은 사이트를 우회할 수 있는 방법을 이용합니다.
// 현재 이용할 수 있는 방법 중에서 막히지 않는 방법은 2개의 요청을 동시에 쓰까서 보내는
// 방법이 있습니다. 이를 위해서, 가짜 dummy host를 앞에 삽입하고, 돌아온 결과에서
// dummy host에 대한 답변을 제거해주어야 합니다.
func over_the_horizon(conn net.Conn) {
	reader := bufio.NewReader(conn)
	packet, host := read_http_request(reader)

	// TODO: 실제 프록시 구현 짜기
	fmt.Printf("HOST: %16s / length: %4d\n", host, len(packet))

	writer := bufio.NewWriter(conn)
	writer.Write([]byte("HTTP/1.0 200 OK\r\n" +
		"Content-Length: 5\r\n" +
		"\r\n" +
		"Hello"))
	writer.Flush()
	conn.Close()
}

func main() {
	fmt.Println("Launching proxy server...")

	// 0. 옵션을 읽어서 적용합니다.
	port := PORT_DEFAULT
	var err error = nil
	switch len(os.Args) {
	case 1:
	case 2:
		port, err = strconv.Atoi(os.Args[1])
		if err != nil {
			usage()
		}
	default:
		usage()
	}

	// 1. TCP 서버를 엽니다.
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		panic("Cannot open tcp server")
	}
	fmt.Println("Proxy is now on port", port)

	// 2. Accept를 하고, 해당 연결을 고루틴으로 처리합니다.
	for {
		conn, err := ln.Accept()
		if err != nil {
			panic("Cannot resolve connection")
		}
		go over_the_horizon(conn)
	}

	// 3. 도달은 불가능하지만, 만약 온다면 서버가 종료된 것입니다.
	fmt.Println("Closing proxy server...")
}
