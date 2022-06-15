// Copyright 2022 SLSA Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sigstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/sigstore/cosign/cmd/cosign/cli/fulcio"
	"github.com/sigstore/cosign/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/pkg/providers"
	"github.com/sigstore/sigstore/pkg/signature/dsse"
	"github.com/slsa-framework/slsa-github-generator/signing"

	intoto "github.com/in-toto/in-toto-golang/in_toto"
)

const (
	defaultFulcioAddr   = "https://v1.fulcio.sigstore.dev"
	defaultOIDCIssuer   = "https://oauth2.sigstore.dev/auth"
	defaultOIDCClientID = "sigstore"
)

// Fulcio is used to sign provenance statements using Fulcio.
type Fulcio struct {
	fulcioAddr   string
	oidcIssuer   string
	oidcClientID string
}

// attestation is a signed attestation.
type attestation struct {
	cert []byte
	att  []byte
}

// Bytes returns the signed attestation as an encoded DSSE JSON envelope.
func (a *attestation) Bytes() []byte {
	return a.att
}

// Cert returns the certificate used to sign the attestation.
func (a *attestation) Cert() []byte {
	return a.cert
}

// NewDefaultFulcio creates a new Fulcio instance using the public Fulcio
// server and public sigstore OIDC issuer.
func NewDefaultFulcio() *Fulcio {
	return NewFulcio(defaultFulcioAddr, defaultOIDCIssuer, defaultOIDCClientID)
}

// NewFulcio creates a new Fulcio instance.
func NewFulcio(fulcioAddr, oidcIssuer, oidcClientID string) *Fulcio {
	return &Fulcio{
		fulcioAddr:   fulcioAddr,
		oidcIssuer:   oidcIssuer,
		oidcClientID: oidcClientID,
	}
}

// Sign signs the given provenance statement and returns the signed
// attestation.
func (s *Fulcio) Sign(ctx context.Context, p *intoto.Statement) (signing.Attestation, error) {
	// Get Fulcio signer
	if !providers.Enabled(ctx) {
		return nil, fmt.Errorf("no auth provider is enabled. Are you running outside of Github Actions?")
	}

	attBytes, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshalling json: %w", err)
	}

	k, err := fulcio.NewSigner(ctx, options.KeyOpts{
		OIDCIssuer:   s.oidcIssuer,
		OIDCClientID: s.oidcClientID,
		FulcioURL:    s.fulcioAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("creating fulcio signer: %w", err)
	}
	wrappedSigner := dsse.WrapSigner(k, intoto.PayloadType)

	signedAtt, err := wrappedSigner.SignMessage(bytes.NewReader(attBytes))
	if err != nil {
		return nil, fmt.Errorf("signing message: %v", err)
	}

	return &attestation{
		att:  signedAtt,
		cert: k.Cert,
	}, nil
}