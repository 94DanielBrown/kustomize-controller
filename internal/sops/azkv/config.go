/*
Copyright 2022 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package azkv

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"sigs.k8s.io/yaml"
)

// LoadAADConfigFromBytes attempts to load the given bytes into the given AADConfig.
// By first decoding it if UTF-16, and then unmarshalling it into the given struct.
// It returns an error for any failure.
func LoadAADConfigFromBytes(b []byte, s *AADConfig) error {
	b, err := decode(b)
	if err != nil {
		return fmt.Errorf("failed to decode Azure authentication file bytes: %w", err)
	}
	if err = yaml.Unmarshal(b, s); err != nil {
		err = fmt.Errorf("failed to unmarshal Azure authentication file: %w", err)
	}
	return err
}

// AADConfig contains the selection of fields from an Azure authentication file
// required for Active Directory authentication.
type AADConfig struct {
	AZConfig
	TenantID                   string `json:"tenantId,omitempty"`
	ClientID                   string `json:"clientId,omitempty"`
	ClientSecret               string `json:"clientSecret,omitempty"`
	ClientCertificate          string `json:"clientCertificate,omitempty"`
	ClientCertificatePassword  string `json:"clientCertificatePassword,omitempty"`
	ClientCertificateSendChain bool   `json:"clientCertificateSendChain,omitempty"`
	AuthorityHost              string `json:"authorityHost,omitempty"`
}

// AZConfig contains the Service Principal fields as generated by `az`.
// Ref: https://docs.microsoft.com/en-us/azure/aks/kubernetes-service-principal?tabs=azure-cli#manually-create-a-service-principal
type AZConfig struct {
	AppID    string `json:"appId,omitempty"`
	Tenant   string `json:"tenant,omitempty"`
	Password string `json:"password,omitempty"`
}

// TokenFromAADConfig attempts to construct a Token using the AADConfig values.
// It detects credentials in the following order:
//
//  - azidentity.ClientSecretCredential when `tenantId`, `clientId` and
//    `clientSecret` fields are found.
//  - azidentity.ClientCertificateCredential when `tenantId`,
//    `clientCertificate` (and optionally `clientCertificatePassword`) fields
//    are found.
//  - azidentity.ClientSecretCredential when AZConfig fields are found.
//  - azidentity.ManagedIdentityCredential for a User ID, when a `clientId`
//    field but no `tenantId` is found.
//
// If no set of credentials is found or the azcore.TokenCredential can not be
// created, an error is returned.
func TokenFromAADConfig(c AADConfig) (_ *Token, err error) {
	var token azcore.TokenCredential
	if c.TenantID != "" && c.ClientID != "" {
		if c.ClientSecret != "" {
			if token, err = azidentity.NewClientSecretCredential(c.TenantID, c.ClientID, c.ClientSecret, &azidentity.ClientSecretCredentialOptions{
				AuthorityHost: c.GetAuthorityHost(),
			}); err != nil {
				return
			}
			return NewToken(token), nil
		}
		if c.ClientCertificate != "" {
			certs, pk, err := azidentity.ParseCertificates([]byte(c.ClientCertificate), []byte(c.ClientCertificatePassword))
			if err != nil {
				return nil, err
			}
			if token, err = azidentity.NewClientCertificateCredential(c.TenantID, c.ClientID, certs, pk, &azidentity.ClientCertificateCredentialOptions{
				SendCertificateChain: c.ClientCertificateSendChain,
				AuthorityHost:        c.GetAuthorityHost(),
			}); err != nil {
				return nil, err
			}
			return NewToken(token), nil
		}
	}

	switch {
	case c.Tenant != "" && c.AppID != "" && c.Password != "":
		if token, err = azidentity.NewClientSecretCredential(c.Tenant, c.AppID, c.Password, &azidentity.ClientSecretCredentialOptions{
			AuthorityHost: c.GetAuthorityHost(),
		}); err != nil {
			return
		}
		return NewToken(token), nil
	case c.ClientID != "":
		if token, err = azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(c.ClientID),
		}); err != nil {
			return
		}
		return NewToken(token), nil
	default:
		return nil, fmt.Errorf("invalid data: requires a '%s' field, a combination of '%s', '%s' and '%s', or '%s', '%s' and '%s'",
			"clientId", "tenantId", "clientId", "clientSecret", "tenantId", "clientId", "clientCertificate")
	}
}

// GetAuthorityHost returns the AuthorityHost, or the Azure Public Cloud
// default.
func (s AADConfig) GetAuthorityHost() azidentity.AuthorityHost {
	if s.AuthorityHost != "" {
		return azidentity.AuthorityHost(s.AuthorityHost)
	}
	return azidentity.AzurePublicCloud
}