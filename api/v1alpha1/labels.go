/*
Copyright 2026.

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

package v1alpha1

// Label keys KIMO applies to the Kubernetes objects (Deployments, Services,
// Pods, NetworkPolicies) it creates on behalf of a ChallengeInstance.
// Shared across internal/controller and internal/api rather than repeated
// as literals — a typo here silently breaks Pod selection and the
// Instance Controller's Pod watch.
const (
	LabelChallenge = "kimo.io/challenge"
	LabelTeam      = "kimo.io/team"
	LabelInstance  = "kimo.io/instance"
)
