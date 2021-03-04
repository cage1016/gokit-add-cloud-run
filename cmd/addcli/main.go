package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"text/tabwriter"

	pb "github.com/cage1016/add/pb/add"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func Filter(arr []string, cond func(string) bool) (res []string) {
	for i := range arr {
		if cond(arr[i]) {
			res = append(res, arr[i])
		}
	}
	return
}

func main() {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	var (
		serverAddr = fs.String("server", "", "Server address (host:port)")
		serverHost = fs.String("server-host", "", "Host name to which server IP should resolve")
		insecure   = fs.Bool("insecure", false, "Skip SSL validation? [false]")
		skipVerify = fs.Bool("skip-verify", false, "Skip server hostname verification in SSL validation [false]")
		method     = fs.String("method", "sum", "sum, concat")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags] <a> <b>")
	fs.Parse(os.Args[1:])
	if len(fs.Args()) != 2 {
		fs.Usage()
		os.Exit(1)
	}

	var opts []grpc.DialOption
	if *serverAddr == "" {
		log.Fatal("-server is empty")
	}
	if *serverHost != "" {
		opts = append(opts, grpc.WithAuthority(*serverHost))
	}
	if *insecure {
		opts = append(opts, grpc.WithInsecure())
	} else {
		cred := credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: *skipVerify,
		})
		opts = append(opts, grpc.WithTransportCredentials(cred))
	}

	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Printf("failed to dial server %s: %v", *serverAddr, err)
	}
	defer conn.Close()

	ctx := context.Background()
	client := pb.NewAddClient(conn)

	switch *method {
	case "sum":
		a, _ := strconv.ParseInt(fs.Args()[0], 10, 64)
		b, _ := strconv.ParseInt(fs.Args()[1], 10, 64)

		res, err := client.Sum(ctx, &pb.SumRequest{A: a, B: b})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%d + %d = %d\n", a, b, res.Res)
	case "concat":
		a := fs.Args()[0]
		b := fs.Args()[1]

		res, err := client.Concat(ctx, &pb.ConcatRequest{A: a, B: b})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%q + %q = %q\n", a, b, res.Res)
	default:
		fmt.Fprintf(os.Stderr, "error: invalid method %q\n", *method)
		os.Exit(1)
	}
}

func usageFor(fs *flag.FlagSet, short string) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, "  %s\n", short)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 2, 2, ' ', 0)
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(w, "\t-%s %s\t%s\n", f.Name, f.DefValue, f.Usage)
		})
		w.Flush()
		fmt.Fprintf(os.Stderr, "\n")
	}
}
