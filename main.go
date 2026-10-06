package main

import (
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed web
var webFS embed.FS

// version is set at build time by the Makefile.
var version = "dev"

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	password := flag.String("password", "", "require this password to browse or upload (optional)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [--port 8080] [--password secret] <folder>\n\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("mediabrowser", version)
		return
	}
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}
	folder := flag.Arg(0)
	// Allow flags after the folder too: mediabrowser ~/Pictures --port 9000
	flag.CommandLine.Parse(flag.Args()[1:])
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	dir, err := filepath.Abs(folder)
	if err != nil {
		log.Fatal(err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		log.Fatalf("%s is not a folder", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		log.Fatal(err)
	}

	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = os.TempDir()
	}
	sum := sha1.Sum([]byte(dir))
	cacheDir := filepath.Join(cacheBase, "mediabrowser", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Fatal(err)
	}

	ffmpeg, _ := exec.LookPath("ffmpeg")

	s := newServer(root, dir, cacheDir, ffmpeg, *password)

	fmt.Printf("mediabrowser %s, serving %s\n", version, dir)
	fmt.Printf("Thumbnail cache: %s\n", cacheDir)
	if ffmpeg == "" {
		fmt.Println("ffmpeg not found on PATH: videos will not get thumbnails")
	}
	if *password != "" {
		fmt.Println("Password protection enabled")
	}
	fmt.Println("Open one of:")
	fmt.Printf("  http://localhost:%d\n", *port)
	ips := lanIPs()
	for _, ip := range ips {
		fmt.Printf("  http://%s:%d\n", ip, *port)
	}
	if len(ips) > 0 {
		url := fmt.Sprintf("http://%s:%d", ips[0], *port)
		fmt.Printf("\nScan to open %s on your phone:\n\n", url)
		printQR(os.Stdout, url)
	}

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), s.routes()))
}

// lanIPs returns the machine's non-loopback IPv4 addresses, private
// (home network) addresses first.
func lanIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var private, other []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		if ipnet.IP.IsPrivate() {
			private = append(private, ipnet.IP.String())
		} else {
			other = append(other, ipnet.IP.String())
		}
	}
	return append(private, other...)
}
