package main

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"encoding/base64"
	"github.com/minio-io/cli"
	"github.com/minio-io/minio/pkg/iodine"
	"github.com/minio-io/minio/pkg/utils/crypto/md5"
	"github.com/minio-io/minio/pkg/utils/log"
	"os"
	"strings"
)

func runCatCmd(ctx *cli.Context) {
	if len(ctx.Args()) != 1 {
		cli.ShowCommandHelpAndExit(ctx, "cat", 1) // last argument is exit code
	}

	config, err := getMcConfig()
	if err != nil {
		log.Debug.Println(iodine.New(err, nil))
		os.Exit(1)
	}

	// Convert arguments to URLs: expand alias, fix format...
	urls, err := getURLs(ctx.Args(), config.Aliases)
	if err != nil {
		switch iodine.ToError(err).(type) {
		case errUnsupportedScheme:
			log.Debug.Println(iodine.New(err, nil))
			os.Exit(1)
		default:
			log.Debug.Println(iodine.New(err, nil))
			os.Exit(1)
		}
	}

	sourceURL := urls[0] // First arg is source
	recursive := isURLRecursive(sourceURL)
	// if recursive strip off the "..."
	if recursive {
		sourceURL = strings.TrimSuffix(sourceURL, recursiveSeparator)
	}

	sourceURLConfigMap := make(map[string]*hostConfig)
	sourceConfig, err := getHostConfig(sourceURL)
	if err != nil {
		log.Debug.Println(iodine.New(err, nil))
		os.Exit(1)
	}
	sourceURLConfigMap[sourceURL] = sourceConfig

	if err != nil {
		log.Debug.Println(iodine.New(err, nil))
		os.Exit(1)
	}
	doCatCmd(mcClientManager{}, os.Stdout, sourceURLConfigMap, globalDebugFlag)
}

func doCatCmd(manager clientManager, writer io.Writer, sourceURLConfigMap map[string]*hostConfig, debug bool) (string, error) {
	for url, config := range sourceURLConfigMap {
		clnt, err := manager.getNewClient(url, config, debug)
		if err != nil {
			// No human readable, will return with non-zero exit
			return "", iodine.New(err, nil)
		}
		reader, size, etag, err := clnt.Get()
		if err != nil {
			// No human readable, will return with non-zero exit
			return "", iodine.New(err, nil)
		}
		wg := &sync.WaitGroup{}
		md5Reader, md5Writer := io.Pipe()
		var actualMd5 []byte
		wg.Add(1)
		go func() {
			actualMd5, _ = md5.Sum(md5Reader)
			// drop error, we'll catch later on if it is important
			wg.Done()
		}()
		teeReader := io.TeeReader(reader, md5Writer)
		_, err = io.CopyN(writer, teeReader, size)
		md5Writer.Close()
		if err != nil {
			return "", iodine.New(err, nil)
		}
		wg.Wait()
		expectedMd5, err := base64.StdEncoding.DecodeString(etag)
		if err != nil {
			return "", iodine.New(errors.New("Unable to read md5sum (etag)"), nil)
		}
		if !bytes.Equal(expectedMd5, actualMd5) {
			return "", iodine.New(errors.New("corruption occurred"), nil)
		}
	}
	return "", nil
}