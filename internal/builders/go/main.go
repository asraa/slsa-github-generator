// Copyright 2022 SLSA Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/slsa-framework/slsa-github-generator/signing/sigstore"

	// Enable the github OIDC auth provider.
	_ "github.com/sigstore/cosign/pkg/providers/github"
	"github.com/sigstore/sigstore/pkg/tuf"

	"github.com/slsa-framework/slsa-github-generator/internal/builders/go/pkg"
)

func usage(p string) {
	panic(fmt.Sprintf(`Usage:
	 %s build [--dry] slsa-releaser.yml
	 %s provenance --binary-name $NAME --digest $DIGEST --command $COMMAND --env $ENV`, p, p))
}

func check(e error) {
	if e != nil {
		fmt.Fprintf(os.Stderr, e.Error())
		os.Exit(1)
	}
}

func runBuild(dry bool, configFile, evalEnvs string) error {
	goc, err := exec.LookPath("go")
	if err != nil {
		return err
	}

	cfg, err := pkg.ConfigFromFile(configFile)
	if err != nil {
		return err
	}
	fmt.Println(cfg)

	gobuild := pkg.GoBuildNew(goc, cfg)

	// Set env variables encoded as arguments.
	err = gobuild.SetArgEnvVariables(evalEnvs)
	if err != nil {
		return err
	}

	err = gobuild.Run(dry)
	if err != nil {
		return err
	}

	return nil
}

func runProvenanceGeneration(ctx context.Context, subject, digest, commands, envs, workingDir string, staging bool) error {
	var s *sigstore.Fulcio
	var r *sigstore.Rekor
	if staging {
		// Initialize TUF with staging mirror.
		err := os.RemoveAll(tuf.TufRootEnv)
		check(err)
		err = tuf.Initialize(ctx, sigstore.StagingTufAddr, sigstore.StagingRoot)
		check(err)
		s = sigstore.NewStagingFulcio()
		r = sigstore.NewStagingRekor()
	} else {
		s = sigstore.NewDefaultFulcio()
		r = sigstore.NewDefaultRekor()
	}

	attBytes, err := pkg.GenerateProvenance(subject, digest,
		commands, envs, workingDir, s, r)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.intoto.jsonl", subject)
	err = ioutil.WriteFile(filename, attBytes, 0o600)
	if err != nil {
		return err
	}

	fmt.Printf("::set-output name=signed-provenance-name::%s\n", filename)

	h, err := computeSHA256(filename)
	if err != nil {
		return err
	}

	fmt.Printf("::set-output name=signed-provenance-sha256::%s\n", h)

	return nil
}

func main() {
	// Build command.
	buildCmd := flag.NewFlagSet("build", flag.ExitOnError)
	buildDry := buildCmd.Bool("dry", false, "dry run of the build without invoking compiler")

	// Provenance command.
	provenanceCmd := flag.NewFlagSet("provenance", flag.ExitOnError)
	provenanceName := provenanceCmd.String("binary-name", "", "untrusted binary name of the artifact built")
	provenanceDigest := provenanceCmd.String("digest", "", "sha256 digest of the untrusted binary")
	provenanceCommand := provenanceCmd.String("command", "", "command used to compile the binary")
	provenanceEnv := provenanceCmd.String("env", "", "env variables used to compile the binary")
	provenanceWorkingDir := provenanceCmd.String("workingDir", "", "working directory used to issue compilation commands")
	stagingRekor := provenanceCmd.Bool("staging", false, "use staging rekor server for signed provenance upload")

	// Expect a sub-command.
	if len(os.Args) < 2 {
		usage(os.Args[0])
	}

	ctx := context.Background()
	switch os.Args[1] {
	case buildCmd.Name():
		check(buildCmd.Parse(os.Args[2:]))
		if len(buildCmd.Args()) < 1 {
			usage(os.Args[0])
		}
		configFile := buildCmd.Args()[0]
		evaluatedEnvs := buildCmd.Args()[1]

		check(runBuild(*buildDry, configFile, evaluatedEnvs))

	case provenanceCmd.Name():
		check(provenanceCmd.Parse(os.Args[2:]))
		// Note: *provenanceEnv may be empty.
		if *provenanceName == "" || *provenanceDigest == "" ||
			*provenanceCommand == "" || *provenanceWorkingDir == "" {
			usage(os.Args[0])
		}

		err := runProvenanceGeneration(ctx, *provenanceName, *provenanceDigest,
			*provenanceCommand, *provenanceEnv, *provenanceWorkingDir, *stagingRekor)
		check(err)

	default:
		fmt.Println("expected 'build' or 'provenance' subcommands")
		os.Exit(1)
	}
}

func computeSHA256(filePath string) (string, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return "", err
	}

	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
