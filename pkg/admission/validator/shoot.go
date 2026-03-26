// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package validator provides admission webhook validators for the Traefik extension.
package validator

import (
	"context"
	"fmt"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// Name is the name of the shoot validator webhook.
	Name = "shoot-validator"
	// ExtensionType is the type of extension being validated.
	ExtensionType = "traefik"
)

// shootValidator validates Shoot resources for the Traefik extension.
type shootValidator struct {
	client  client.Client
	decoder runtime.Decoder
}

// NewShootValidatorWebhook creates a new webhook for validating Shoot resources.
// It ensures that the Traefik extension can only be enabled for shoots with
// purpose "evaluation".
func NewShootValidatorWebhook(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	decoder := serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder()

	return extensionswebhook.New(mgr, extensionswebhook.Args{
		Provider: ExtensionType,
		Name:     Name,
		Path:     "/webhooks/validate-shoot-traefik",
		Target:   extensionswebhook.TargetSeed,
		Validators: map[extensionswebhook.Validator][]extensionswebhook.Type{
			NewShootValidator(mgr.GetClient(), decoder): {
				{Obj: &gardencorev1beta1.Shoot{}},
			},
		},
	})
}

// NewShootValidator creates a new shoot validator.
func NewShootValidator(c client.Client, decoder runtime.Decoder) extensionswebhook.Validator {
	return &shootValidator{
		client:  c,
		decoder: decoder,
	}
}

// Validate validates the given object (Shoot) on create and update operations.
func (v *shootValidator) Validate(ctx context.Context, newClient, old client.Object) error {
	shoot, ok := newClient.(*gardencorev1beta1.Shoot)
	if !ok {
		return fmt.Errorf("expected *gardencorev1beta1.Shoot but got %T", newClient)
	}

	return v.validateShoot(shoot)
}

// validateShoot validates that if the Traefik extension is enabled,
// the shoot must have purpose "evaluation".
func (v *shootValidator) validateShoot(shoot *gardencorev1beta1.Shoot) error {
	// Check if the Traefik extension is configured and enabled
	hasTraefikExtension := false
	for _, ext := range shoot.Spec.Extensions {
		if ext.Type == ExtensionType {
			if ext.Disabled != nil && *ext.Disabled {
				return nil
			}
			hasTraefikExtension = true

			break
		}
	}

	// If no Traefik extension, validation passes
	if !hasTraefikExtension {
		return nil
	}

	// Validate that the shoot purpose is "evaluation"
	if shoot.Spec.Purpose == nil || *shoot.Spec.Purpose != gardencorev1beta1.ShootPurposeEvaluation {
		purposeStr := "nil"
		if shoot.Spec.Purpose != nil {
			purposeStr = string(*shoot.Spec.Purpose)
		}

		return fmt.Errorf(
			"traefik extension can only be enabled for shoots with purpose 'evaluation'. "+
				"Current purpose: %s. Traefik acts as a replacement for the nginx ingress controller "+
				"and is only supported for evaluation clusters",
			purposeStr,
		)
	}

	return nil
}
