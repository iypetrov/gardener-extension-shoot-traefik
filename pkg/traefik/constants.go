// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package traefik

const (
	// Namespace is the namespace where Traefik will be deployed in the shoot cluster.
	Namespace = "kube-system"

	// DeploymentName is the name of the Traefik deployment.
	DeploymentName = "traefik"

	// ServiceAccountName is the name of the service account for Traefik.
	ServiceAccountName = "traefik"

	// ManagedResourceName is the name of the ManagedResource for Traefik.
	ManagedResourceName = "extension-traefik"

	// ImageName is the name of the Traefik image in the image vector.
	ImageName = "traefik"

	// SeedManagedResourceName is the name of the seed-class ManagedResource
	// that contains the DNSRecord for the Traefik ingress wildcard domain.
	SeedManagedResourceName = "extension-traefik-ingress-dns"
)
