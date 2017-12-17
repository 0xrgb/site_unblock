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

const DUMMY_PACKET = "GET HTTP/1.1\r\n" +
	"Host: tsumugi.shiraishi.millionlive.theaterdays\r\n" +
	"\r\n"

// usage는 이 프록시의 사용법을 stdout에 출력하고 프로그램을 종료합니다.
func usage() {
	fmt.Println("Usage: site_unblock <PORT>")
	fmt.Println("[+] Default PORT is ", PORT_DEFAULT)
	os.Exit(-1)
}

// read_http_response는 bufio.Reader를 하나 받아, 서버로부터 들어오는 HTTP 답변을
// 하나 읽어냅니다. 그 후, 읽어낸만큼의 패킷이 담긴 []byte를 리턴합니다.
// 문제가 생겼을 경우, panic을 발생시키거나 nil을 리턴합니다.
func read_http_response(reader *bufio.Reader) []byte {
	packet := make([]byte, 0, 1600)
	content_length := 0
	content_length_bytes := []byte("Content-Length: ")
	for {
		// TODO: 연결이 중간에 끊겼을 때 제대로 처리하기.
		header_line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Println("[i] Connection ended in http response:", err)
			return nil
		}

		packet = append(packet, header_line...)
		if len(header_line) == 2 {
			break
		}

		if bytes.HasPrefix(header_line, content_length_bytes) {
			if content_length != 0 {
				panic("Wrong http respond(too many content-length)")
			}
			content_length, err = strconv.Atoi(
				string(header_line[16 : len(header_line)-2]))
			if err != nil {
				panic("Wrong http respond(bad content-length)")
			}
		}
	}

	// 헤더에 Content-Length가 없다면, length가 0인 것이니까 멈춥니다.
	if content_length == 0 {
		return packet
	}

	// Content-Length만큼의 byte를 추가로 읽습니다.
	tmp := make([]byte, content_length)
	_, err := io.ReadFull(reader, tmp)
	if err != nil {
		panic("Wrong http respond(short content)")
	}

	packet = append(packet, tmp...)
	return packet
}

// read_http_request는 bufio.Reader를 하나 받아, 클라이언트가 요청하는 HTTP 질의을
// 하나 읽어냅니다. 그 후, 읽어낸만큼의 패킷이 담긴 []byte와 해당 HTTP request의 HOST를
// 반환합니다.
// 패킷 안에 Content-Length가 있어야 작동합니다.
// 문제가 생겼을 경우, panic을 발생시키거나 nil을 리턴합니다.
func read_http_request(reader *bufio.Reader) ([]byte, []byte) {
	var host []byte = nil
	content_length := 0
	packet := make([]byte, 0, 560)

	hosts_bytes := []byte("Host: ")
	content_length_bytes := []byte("Content-Length: ")

	// 1. 헤더 읽기
	for {
		// 각각의 헤더 한 줄은 "\r\n"으로 끝나므로 '\n'을 찾아줍니다.
		header_line, err := reader.ReadBytes('\n')
		if err != nil {
			// 헤더도 다 못 읽었는데 통신이 끝났습니다.
			// 아니면, 애초부터 통신이 끊겼을 가능성이 있습니다.
			// 어느 경우든, 실패한 통신이므로 nil을 반환합니다.
			return nil, nil
		}

		// 반환할 패킷에 읽은 줄을 추가합니다.
		packet = append(packet, header_line...)

		// 빈 줄이 나왔으므로 헤더가 끝났습니다.
		if len(header_line) == 2 {
			break
		}

		// Host
		if bytes.HasPrefix(header_line, hosts_bytes) {
			if host != nil {
				panic("Wrong http request(too many host)")
			}
			host = header_line[6 : len(header_line)-2]
		}

		// Content-length
		if bytes.HasPrefix(header_line, content_length_bytes) {
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

	// 헤더에 Host가 없었다면, 잘못된 요청이므로 panic 합니다.
	if host == nil {
		panic("Wrong http request(no host)")
	}

	// 헤더에 Content-Length가 없다면 GET과 같은 헤더만 있는 요청이므로 여기서
	// 중단해도 됩니다.
	if content_length == 0 {
		return packet, host
	}

	// Content-Length만큼의 byte를 추가로 읽습니다.
	tmp := make([]byte, content_length)
	_, err := io.ReadFull(reader, tmp)
	if err != nil {
		panic("Wrong http request(short content)")
	}

	packet = append(packet, tmp...)
	return packet, host
}

// over_the_horizon은 http connection을 받아 HTTP 통신을 하게 합니다.
// 이 과정에서 warning.or.kr과 같은 사이트를 우회할 수 있는 방법을 이용합니다.
// 현재 이용할 수 있는 방법 중에서 막히지 않는 방법은 2개의 요청을 동시에 쓰까서 보내는
// 방법이 있습니다. 이를 위해서, 가짜 dummy host를 앞에 삽입하고, 돌아온 결과에서
// dummy host에 대한 답변을 제거해주어야 합니다.
func over_the_horizon(conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	buf := make([]byte, 0, 1600)
	buf = append(buf, []byte(DUMMY_PACKET)...)
	dummy_len := len(buf)

	for {
		packet, host := read_http_request(reader)
		if packet == nil {
			fmt.Println("Connection end")
			conn.Close()
			break
		}

		// HTTP request 파싱 성공
		fmt.Printf("HOST: %s / length: %d\n", string(host), len(packet))

		// 해당 호스트와 통신하기 위해 소켓을 엽니다.
		// 이 때, 만약 host에 포트번호가 적혀있지 않다면 80번 포트를 이용해야
		// 하므로, ":http"을 뒤에 붙여주어야 합니다. 대부분의 HTTP 통신은 다른
		// 포트를 이용하지 않으므로, 일단 무조건 붙여주도록 합니다.
		// TODO: host뒤에 포트번호가 적혀있는지 체크하기
		dial, err := net.Dial("tcp", string(host) + ":http")
		if err != nil {
			fmt.Println("[i] Cannot open socket:", err)
			continue
		}

		// Dummy와 합쳐서 보낸다
		nbuf := append(buf[:dummy_len], packet...)
		writer_dial := bufio.NewWriter(dial)
		_, err = writer_dial.Write(nbuf)
		if err != nil {
			fmt.Println("[i] Send failed:", err)
			dial.Close()
			continue
		}
		writer_dial.Flush()

		// 답장 처리
		// 앞의 dummy에 대한 답장을 지워줘야 클라이언트가 올바른 패킷을 받는다.
		reader_dial := bufio.NewReader(dial)
		// should check (_ == nil), but its ok
		_ = read_http_response(reader_dial)
		chk := read_http_response(reader_dial)
		if chk == nil {
			fmt.Println("[i] Recieve failed")
			dial.Close()
			continue
		}
		dial.Close()

		// 마지막으로 클라이언트에게 답변을 그대로 돌려준다
		_, err = writer.Write(chk)
		if err != nil {
			fmt.Println("[i] Relay failed:", err)
		}
		writer.Flush()
	}
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
