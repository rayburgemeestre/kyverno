package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	kyverno "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/kyverno/kyverno/pkg/config"
	"github.com/kyverno/kyverno/pkg/cosign"
	engineapi "github.com/kyverno/kyverno/pkg/engine/api"
	enginecontext "github.com/kyverno/kyverno/pkg/engine/context"
	"github.com/kyverno/kyverno/pkg/engine/context/resolvers"
	"github.com/kyverno/kyverno/pkg/engine/internal"
	"github.com/kyverno/kyverno/pkg/engine/utils"
	"github.com/kyverno/kyverno/pkg/logging"
	"github.com/kyverno/kyverno/pkg/registryclient"
	kubeutils "github.com/kyverno/kyverno/pkg/utils/kube"
	"gotest.tools/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

var testPolicyGood = `{
  "apiVersion": "kyverno.io/v1",
  "kind": "ClusterPolicy",
  "metadata": {
    "name": "attest"
  },
  "spec": {
    "rules": [
      {
        "name": "attest",
        "match": {
          "resources": {
            "kinds": [
              "Pod"
            ]
          }
        },
        "verifyImages": [
          {
            "image": "*",
            "key": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEHMmDjK65krAyDaGaeyWNzgvIu155JI50B2vezCw8+3CVeE0lJTL5dbL3OP98Za0oAEBJcOxky8Riy/XcmfKZbw==\n-----END PUBLIC KEY-----",
            "attestations": [
              {
                "predicateType": "https://example.com/CodeReview/v1",
				"attestors": [
					{
						"entries": [
							{
								"keys": {
									"publicKeys": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEHMmDjK65krAyDaGaeyWNzgvIu155JI50B2vezCw8+3CVeE0lJTL5dbL3OP98Za0oAEBJcOxky8Riy/XcmfKZbw==\n-----END PUBLIC KEY-----"
								}
							}
						]
					}
				],
                "conditions": [
                  {
                    "all": [
                      {
                        "key": "{{ repo.uri }}",
                        "operator": "Equals",
                        "value": "https://github.com/example/my-project"
                      },
                      {
                        "key": "{{ repo.branch }}",
                        "operator": "Equals",
                        "value": "main"
                      }
                    ]
                  }
                ]
              }
            ]
          }
        ]
      }
    ]
  }
}`

var testPolicyBad = `{
  "apiVersion": "kyverno.io/v1",
  "kind": "ClusterPolicy",
  "metadata": {
    "name": "attest"
  },
  "spec": {
    "rules": [
      {
        "name": "attest",
        "match": {
          "resources": {
            "kinds": [
              "Pod"
            ]
          }
        },
        "verifyImages": [
          {
            "image": "*",
            "key": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEHMmDjK65krAyDaGaeyWNzgvIu155JI50B2vezCw8+3CVeE0lJTL5dbL3OP98Za0oAEBJcOxky8Riy/XcmfKZbw==\n-----END PUBLIC KEY-----",
            "attestations": [
              {
                "predicateType": "https://example.com/CodeReview/v1",
                "conditions": [
                  {
                    "all": [
                      {
                        "key": "{{ repo.uri }}",
                        "operator": "Equals",
                        "value": "https://github.com/example/my-project"
                      },
                      {
                        "key": "{{ repo.branch }}",
                        "operator": "Equals",
                        "value": "prod"
                      }
                    ]
                  }
                ]
              }
            ]
          }
        ]
      }
    ]
  }
}`

var testResource = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {
     "name": "test",
     "annotations": {}
  },
  "spec": {
    "containers": [
      {
        "name": "pause2",
        "image": "ghcr.io/jimbugwadia/pause2"
      }
    ]
  }
}`

var attestationPayloads = [][]byte{
	[]byte(`{"payloadType":"https://example.com/CodeReview/v1","payload":"eyJfdHlwZSI6Imh0dHBzOi8vaW4tdG90by5pby9TdGF0ZW1lbnQvdjAuMSIsInByZWRpY2F0ZVR5cGUiOiJodHRwczovL2V4YW1wbGUuY29tL0NvZGVSZXZpZXcvdjEiLCJzdWJqZWN0IjpbeyJuYW1lIjoiZ2hjci5pby9qaW1idWd3YWRpYS9wYXVzZTIiLCJkaWdlc3QiOnsic2hhMjU2IjoiYjMxYmZiNGQwMjEzZjI1NGQzNjFlMDA3OWRlYWFlYmVmYTRmODJiYTdhYTc2ZWY4MmU5MGI0OTM1YWQ1YjEwNSJ9fV0sInByZWRpY2F0ZSI6eyJhdXRob3IiOiJtYWlsdG86YWxpY2VAZXhhbXBsZS5jb20iLCJyZXBvIjp7ImJyYW5jaCI6Im1haW4iLCJ0eXBlIjoiZ2l0IiwidXJpIjoiaHR0cHM6Ly9naXRodWIuY29tL2V4YW1wbGUvbXktcHJvamVjdCJ9LCJyZXZpZXdlcnMiOlsibWFpbHRvOmJvYkBleGFtcGxlLmNvbSJdfX0=","signatures":[{"keyid":"","sig":"MEYCIQCrEr+vgPDmNCrqGDE/4z9iMLmCXMXcDlGKtSoiuMTSFgIhAN2riBaGk4accWzVl7ypi1XTRxyrPYHst8DesugPXgOf"}]}`),
	[]byte(`{"payloadType":"cosign.sigstore.dev/attestation/v1","payload":"eyJfdHlwZSI6Imh0dHBzOi8vaW4tdG90by5pby9TdGF0ZW1lbnQvdjAuMSIsInByZWRpY2F0ZVR5cGUiOiJjb3NpZ24uc2lnc3RvcmUuZGV2L2F0dGVzdGF0aW9uL3YxIiwic3ViamVjdCI6W3sibmFtZSI6ImdoY3IuaW8vamltYnVnd2FkaWEvcGF1c2UyIiwiZGlnZXN0Ijp7InNoYTI1NiI6ImIzMWJmYjRkMDIxM2YyNTRkMzYxZTAwNzlkZWFhZWJlZmE0ZjgyYmE3YWE3NmVmODJlOTBiNDkzNWFkNWIxMDUifX1dLCJwcmVkaWNhdGUiOnsiRGF0YSI6ImhlbGxvIVxuIiwiVGltZXN0YW1wIjoiMjAyMS0xMC0wNVQwNToxODoxMVoifX0=","signatures":[{"keyid":"","sig":"MEQCIF5r9lf55rnYNPByZ9v6bortww694UEPvmyBIelIDYbIAiBNTGX4V64Oj6jZVRpkJQRxdzKUPYqC5GZTb4oS6eQ6aQ=="}]}`),
	[]byte(`{"payloadType":"https://example.com/CodeReview/v1","payload":"eyJfdHlwZSI6Imh0dHBzOi8vaW4tdG90by5pby9TdGF0ZW1lbnQvdjAuMSIsInByZWRpY2F0ZVR5cGUiOiJodHRwczovL2V4YW1wbGUuY29tL0NvZGVSZXZpZXcvdjEiLCJzdWJqZWN0IjpbeyJuYW1lIjoiZ2hjci5pby9qaW1idWd3YWRpYS9wYXVzZTIiLCJkaWdlc3QiOnsic2hhMjU2IjoiYjMxYmZiNGQwMjEzZjI1NGQzNjFlMDA3OWRlYWFlYmVmYTRmODJiYTdhYTc2ZWY4MmU5MGI0OTM1YWQ1YjEwNSJ9fV0sInByZWRpY2F0ZSI6eyJhdXRob3IiOiJtYWlsdG86YWxpY2VAZXhhbXBsZS5jb20iLCJyZXBvIjp7ImJyYW5jaCI6Im1haW4iLCJ0eXBlIjoiZ2l0IiwidXJpIjoiaHR0cHM6Ly9naXRodWIuY29tL2V4YW1wbGUvbXktcHJvamVjdCJ9LCJyZXZpZXdlcnMiOlsibWFpbHRvOmJvYkBleGFtcGxlLmNvbSJdfX0=","signatures":[{"keyid":"","sig":"MEUCIEeZbdBEFQzWqiMhB+SJgM6yFppUuQSKrpOIX1mxLDmRAiEA8pXqFq0GVc9LKhPzrnJRZhSruDNiKbiLHG5x7ETFyY8="}]}`),
}

var signaturePayloads = [][]byte{
	[]byte(`{"critical":{"identity":{"docker-reference":"ghcr.io/kyverno/test-verify-image"},"image":{"docker-manifest-digest":"sha256:b31bfb4d0213f254d361e0079deaaebefa4f82ba7aa76ef82e90b4935ad5b105"},"type":"cosign container image signature"},"optional":null}`),
}

var cfg = config.NewDefaultConfiguration()

func testVerifyAndPatchImages(
	ctx context.Context,
	rclient registryclient.Client,
	cmResolver engineapi.ConfigmapResolver,
	pContext engineapi.PolicyContext,
	cfg config.Configuration,
) (*engineapi.EngineResponse, *engineapi.ImageVerificationMetadata) {
	e := NewEngine(
		cfg,
		nil,
		rclient,
		LegacyContextLoaderFactory(cmResolver),
		nil,
	)
	return e.VerifyAndPatchImages(
		ctx,
		pContext,
	)
}

func Test_CosignMockAttest(t *testing.T) {
	policyContext := buildContext(t, testPolicyGood, testResource, "")
	err := cosign.SetMock("ghcr.io/jimbugwadia/pause2:latest", attestationPayloads)
	assert.NilError(t, err)

	er, ivm := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(er.PolicyResponse.Rules), 1)
	assert.Equal(t, er.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass,
		fmt.Sprintf("expected: %v, got: %v, failure: %v",
			engineapi.RuleStatusPass, er.PolicyResponse.Rules[0].Status, er.PolicyResponse.Rules[0].Message))
	assert.Equal(t, ivm.IsEmpty(), false)
	assert.Equal(t, ivm.IsVerified("ghcr.io/jimbugwadia/pause2:latest"), true)
}

func Test_CosignMockAttest_fail(t *testing.T) {
	policyContext := buildContext(t, testPolicyBad, testResource, "")
	err := cosign.SetMock("ghcr.io/jimbugwadia/pause2:latest", attestationPayloads)
	assert.NilError(t, err)

	er, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(er.PolicyResponse.Rules), 1)
	assert.Equal(t, er.PolicyResponse.Rules[0].Status, engineapi.RuleStatusFail)
}

func buildContext(t *testing.T, policy, resource string, oldResource string) *PolicyContext {
	var cpol kyverno.ClusterPolicy
	err := json.Unmarshal([]byte(policy), &cpol)
	assert.NilError(t, err)

	resourceUnstructured, err := kubeutils.BytesToUnstructured([]byte(resource))
	assert.NilError(t, err)

	ctx := enginecontext.NewContext()
	err = enginecontext.AddResource(ctx, []byte(resource))
	assert.NilError(t, err)

	policyContext := &PolicyContext{
		policy:      &cpol,
		jsonContext: ctx,
		newResource: *resourceUnstructured,
	}

	if oldResource != "" {
		oldResourceUnstructured, err := kubeutils.BytesToUnstructured([]byte(oldResource))
		assert.NilError(t, err)

		err = enginecontext.AddOldResource(ctx, []byte(oldResource))
		assert.NilError(t, err)

		policyContext.oldResource = *oldResourceUnstructured
	}

	if err := ctx.AddImageInfos(resourceUnstructured, cfg); err != nil {
		t.Errorf("unable to add image info to variables context: %v", err)
		t.Fail()
	}

	return policyContext
}

var testSampleSingleKeyPolicy = `
{
    "apiVersion": "kyverno.io/v1",
    "kind": "ClusterPolicy",
    "metadata": {
        "name": "check-image",
        "annotations": {
            "pod-policies.kyverno.io/autogen-controllers": "none"
        }
    },
    "spec": {
        "validationFailureAction": "enforce",
        "background": false,
        "webhookTimeoutSeconds": 30,
        "failurePolicy": "Fail",
        "rules": [
            {
                "name": "check-signature",
                "match": {
                    "resources": {
                        "kinds": [
                            "Pod"
                        ]
                    }
                },
                "verifyImages": [
                    {
                        "imageReferences": [
                            "ghcr.io/kyverno/test-verify-image:*"
                        ],
                        "attestors": [
                            {
                                "entries": [
                                    {
                                        "keys": {
                                            "publicKeys": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE8nXRh950IZbRj8Ra/N9sbqOPZrfM\n5/KAQN0/KjHcorm/J5yctVd7iEcnessRQjU917hmKO6JWVGHpDguIyakZA==\n-----END PUBLIC KEY-----"
                                        }
                                    }
                                ]
                            }
                        ]
                    }
                ]
            }
        ]
    }
}
`

var testSampleMultipleKeyPolicy = `
{
    "apiVersion": "kyverno.io/v1",
    "kind": "ClusterPolicy",
    "metadata": {
        "name": "check-image",
        "annotations": {
            "pod-policies.kyverno.io/autogen-controllers": "none"
        }
    },
    "spec": {
        "validationFailureAction": "enforce",
        "background": false,
        "webhookTimeoutSeconds": 30,
        "failurePolicy": "Fail",
        "rules": [
            {
                "name": "check-signature",
                "match": {
                    "resources": {
                        "kinds": [
                            "Pod"
                        ]
                    }
                },
                "verifyImages": [
                    {
                        "imageReferences": [
                            "ghcr.io/kyverno/test-verify-image:*"
                        ],
                        "attestors": [
                            {
                                "count": COUNT,
                                "entries": [
                                    {
                                        "keys": {
                                            "publicKeys": "KEY1"
                                        }
                                    },
                                    {
                                        "keys": {
                                            "publicKeys": "KEY2"
                                        }
                                    }
                                ]
                            }
                        ]
                    }
                ]
            }
        ]
    }
}
`

var testConfigMapMissing = `{
    "apiVersion": "kyverno.io/v1",
    "kind": "ClusterPolicy",
    "metadata": {
        "annotations": {
            "pod-policies.kyverno.io/autogen-controllers": "none"
        },
        "name": "image-verify-polset"
    },
    "spec": {
        "background": false,
        "failurePolicy": "Fail",
        "rules": [
            {
                "context": [
                    {
                        "configMap": {
                            "name": "myconfigmap",
                            "namespace": "mynamespace"
                        },
                        "name": "myconfigmap"
                    }
                ],
                "match": {
                    "any": [
                        {
                            "resources": {
                                "kinds": [
                                    "Pod"
                                ]
                            }
                        }
                    ]
                },
                "name": "image-verify-pol1",
                "verifyImages": [
                    {
                        "imageReferences": [
                            "ghcr.io/*"
                        ],
                        "mutateDigest": false,
                        "verifyDigest": false,
                        "attestors": [
                            {
                                "entries": [
                                    {
                                        "keys": {
                                            "publicKeys": "{{myconfigmap.data.configmapkey}}"
                                        }
                                    }
                                ]
                            }
                        ]
                    }
                ]
            }
        ],
        "validationFailureAction": "Audit",
        "webhookTimeoutSeconds": 30
    }
}`

var testSampleResource = `{
  "apiVersion": "v1",
  "kind": "Pod",
  "metadata": {"name": "test"},
  "spec": {
    "containers": [
      {
        "name": "pause2",
        "image": "ghcr.io/kyverno/test-verify-image:signed"
      }
    ]
  }
}`

var testConfigMapMissingResource = `{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "labels": {
            "run": "test"
        },
        "name": "test"
    },
    "spec": {
        "containers": [
            {
                "image": "nginx:latest",
                "name": "test",
                "resources": {}
            }
        ]
    }
}`

var (
	testVerifyImageKey = `-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE8nXRh950IZbRj8Ra/N9sbqOPZrfM5/KAQN0/KjHcorm/J5yctVd7iEcnessRQjU917hmKO6JWVGHpDguIyakZA==\n-----END PUBLIC KEY-----\n`
	testOtherKey       = `-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEpNlOGZ323zMlhs4bcKSpAKQvbcWi5ZLRmijm6SqXDy0Fp0z0Eal+BekFnLzs8rUXUaXlhZ3hNudlgFJH+nFNMw==\n-----END PUBLIC KEY-----\n`
)

func Test_ConfigMapMissingSuccess(t *testing.T) {
	policyContext := buildContext(t, testConfigMapMissing, testConfigMapMissingResource, "")
	cosign.ClearMock()
	err, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(err.PolicyResponse.Rules), 1)
	assert.Equal(t, err.PolicyResponse.Rules[0].Status, engineapi.RuleStatusSkip, err.PolicyResponse.Rules[0].Message)
}

func Test_ConfigMapMissingFailure(t *testing.T) {
	ghcrImage := strings.Replace(testConfigMapMissingResource, "nginx:latest", "ghcr.io/kyverno/test-verify-image:signed", -1)
	policyContext := buildContext(t, testConfigMapMissing, ghcrImage, "")
	resolver, err := resolvers.NewClientBasedResolver(kubefake.NewSimpleClientset())
	assert.NilError(t, err)
	cosign.ClearMock()
	resp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), resolver, policyContext, cfg)
	assert.Equal(t, len(resp.PolicyResponse.Rules), 1)
	assert.Equal(t, resp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusError, resp.PolicyResponse.Rules[0].Message)
}

func Test_SignatureGoodSigned(t *testing.T) {
	policyContext := buildContext(t, testSampleSingleKeyPolicy, testSampleResource, "")
	policyContext.policy.GetSpec().Rules[0].VerifyImages[0].MutateDigest = true
	cosign.ClearMock()
	engineResp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass, engineResp.PolicyResponse.Rules[0].Message)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules[0].Patches), 1)
	patch := engineResp.PolicyResponse.Rules[0].Patches[0]
	assert.Equal(t, string(patch), "{\"op\":\"replace\",\"path\":\"/spec/containers/0/image\",\"value\":\"ghcr.io/kyverno/test-verify-image:signed@sha256:b31bfb4d0213f254d361e0079deaaebefa4f82ba7aa76ef82e90b4935ad5b105\"}")
}

func Test_SignatureUnsigned(t *testing.T) {
	cosign.ClearMock()
	unsigned := strings.Replace(testSampleResource, ":signed", ":unsigned", -1)
	policyContext := buildContext(t, testSampleSingleKeyPolicy, unsigned, "")
	engineResp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusFail, engineResp.PolicyResponse.Rules[0].Message)
}

func Test_SignatureWrongKey(t *testing.T) {
	cosign.ClearMock()
	otherKey := strings.Replace(testSampleResource, ":signed", ":signed-by-someone-else", -1)
	policyContext := buildContext(t, testSampleSingleKeyPolicy, otherKey, "")
	engineResp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusFail, engineResp.PolicyResponse.Rules[0].Message)
}

func Test_SignaturesMultiKey(t *testing.T) {
	cosign.ClearMock()
	policy := strings.Replace(testSampleMultipleKeyPolicy, "KEY1", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "KEY2", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "COUNT", "0", -1)
	policyContext := buildContext(t, policy, testSampleResource, "")
	engineResp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass, engineResp.PolicyResponse.Rules[0].Message)
}

func Test_SignaturesMultiKeyFail(t *testing.T) {
	cosign.ClearMock()
	policy := strings.Replace(testSampleMultipleKeyPolicy, "KEY1", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "COUNT", "0", -1)
	policyContext := buildContext(t, policy, testSampleResource, "")
	engineResp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusFail, engineResp.PolicyResponse.Rules[0].Message)
}

func Test_SignaturesMultiKeyOneGoodKey(t *testing.T) {
	cosign.ClearMock()
	policy := strings.Replace(testSampleMultipleKeyPolicy, "KEY1", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "KEY2", testOtherKey, -1)
	policy = strings.Replace(policy, "COUNT", "1", -1)
	policyContext := buildContext(t, policy, testSampleResource, "")
	engineResp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(engineResp.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass, engineResp.PolicyResponse.Rules[0].Message)
}

func Test_SignaturesMultiKeyZeroGoodKey(t *testing.T) {
	cosign.ClearMock()
	policy := strings.Replace(testSampleMultipleKeyPolicy, "KEY1", testOtherKey, -1)
	policy = strings.Replace(policy, "KEY2", testOtherKey, -1)
	policy = strings.Replace(policy, "COUNT", "1", -1)
	policyContext := buildContext(t, policy, testSampleResource, "")
	resp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(resp.PolicyResponse.Rules), 1)
	assert.Equal(t, resp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusFail, resp.PolicyResponse.Rules[0].Message)
}

func Test_RuleSelectorImageVerify(t *testing.T) {
	cosign.ClearMock()

	policyContext := buildContext(t, testSampleSingleKeyPolicy, testSampleResource, "")
	rule := newStaticKeyRule("match-all", "*", testOtherKey)
	spec := policyContext.policy.GetSpec()
	spec.Rules = append(spec.Rules, *rule)

	applyAll := kyverno.ApplyAll
	spec.ApplyRules = &applyAll

	resp, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(resp.PolicyResponse.Rules), 2)
	assert.Equal(t, resp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass, resp.PolicyResponse.Rules[0].Message)
	assert.Equal(t, resp.PolicyResponse.Rules[1].Status, engineapi.RuleStatusFail, resp.PolicyResponse.Rules[1].Message)

	applyOne := kyverno.ApplyOne
	spec.ApplyRules = &applyOne
	resp, _ = testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(resp.PolicyResponse.Rules), 1)
	assert.Equal(t, resp.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass, resp.PolicyResponse.Rules[0].Message)
}

func newStaticKeyRule(name, imageReference, key string) *kyverno.Rule {
	return &kyverno.Rule{
		Name: name,
		MatchResources: kyverno.MatchResources{
			All: kyverno.ResourceFilters{
				{
					ResourceDescription: kyverno.ResourceDescription{
						Kinds: []string{"Pod"},
					},
				},
			},
		},
		VerifyImages: []kyverno.ImageVerification{
			{
				ImageReferences: []string{"*"},
				Attestors: []kyverno.AttestorSet{
					{
						Entries: []kyverno.Attestor{
							{
								Keys: &kyverno.StaticKeyAttestor{
									PublicKeys: key,
								},
							},
						},
					},
				},
			},
		},
	}
}

var testNestedAttestorPolicy = `
{
    "apiVersion": "kyverno.io/v1",
    "kind": "ClusterPolicy",
    "metadata": {
        "name": "check-image-keyless",
        "annotations": {
            "pod-policies.kyverno.io/autogen-controllers": "none"
        }
    },
    "spec": {
        "validationFailureAction": "enforce",
        "background": false,
        "webhookTimeoutSeconds": 30,
        "failurePolicy": "Fail",
        "rules": [
            {
                "name": "check-image-keyless",
                "match": {
                    "resources": {
                        "kinds": [
                            "Pod"
                        ]
                    }
                },
                "verifyImages": [
                    {
                        "imageReferences": [
                            "ghcr.io/kyverno/test-verify-image:*"
                        ],
                        "attestors": [
                            {
                                "count": COUNT,
                                "entries": [
                                    {
                                        "keys": {
                                            "publicKeys": "KEY1"
                                        }
                                    },
                                    {
                                        "attestor": {
                                            "entries": [
                                                {
                                                    "keys": {
                                                        "publicKeys": "KEY2"
                                                    }
                                                }
                                            ]
                                        }
                                    }
                                ]
                            }
                        ]
                    }
                ]
            }
        ]
    }
}
`

func Test_NestedAttestors(t *testing.T) {
	cosign.ClearMock()

	policy := strings.Replace(testNestedAttestorPolicy, "KEY1", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "KEY2", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "COUNT", "0", -1)
	policyContext := buildContext(t, policy, testSampleResource, "")
	err, _ := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(err.PolicyResponse.Rules), 1)
	assert.Equal(t, err.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass)

	policy = strings.Replace(testNestedAttestorPolicy, "KEY1", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "KEY2", testOtherKey, -1)
	policy = strings.Replace(policy, "COUNT", "0", -1)
	policyContext = buildContext(t, policy, testSampleResource, "")
	err, _ = testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(err.PolicyResponse.Rules), 1)
	assert.Equal(t, err.PolicyResponse.Rules[0].Status, engineapi.RuleStatusFail)

	policy = strings.Replace(testNestedAttestorPolicy, "KEY1", testVerifyImageKey, -1)
	policy = strings.Replace(policy, "KEY2", testOtherKey, -1)
	policy = strings.Replace(policy, "COUNT", "1", -1)
	policyContext = buildContext(t, policy, testSampleResource, "")
	err, _ = testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Equal(t, len(err.PolicyResponse.Rules), 1)
	assert.Equal(t, err.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass)
}

func Test_ExpandKeys(t *testing.T) {
	as := internal.ExpandStaticKeys(createStaticKeyAttestorSet("", true, false, false))
	assert.Equal(t, 1, len(as.Entries))

	as = internal.ExpandStaticKeys(createStaticKeyAttestorSet(testOtherKey, true, false, false))
	assert.Equal(t, 1, len(as.Entries))

	as = internal.ExpandStaticKeys(createStaticKeyAttestorSet(testOtherKey+testOtherKey+testOtherKey, true, false, false))
	assert.Equal(t, 3, len(as.Entries))

	as = internal.ExpandStaticKeys(createStaticKeyAttestorSet("", false, true, false))
	assert.Equal(t, 1, len(as.Entries))
	assert.DeepEqual(t, &kyverno.SecretReference{Name: "testsecret", Namespace: "default"},
		as.Entries[0].Keys.Secret)

	as = internal.ExpandStaticKeys(createStaticKeyAttestorSet("", false, false, true))
	assert.Equal(t, 1, len(as.Entries))
	assert.DeepEqual(t, "gcpkms://projects/test_project_id/locations/asia-south1/keyRings/test_key_ring_name/cryptoKeys/test_key_name/versions/1", as.Entries[0].Keys.KMS)

	as = internal.ExpandStaticKeys((createStaticKeyAttestorSet(testOtherKey, true, true, false)))
	assert.Equal(t, 2, len(as.Entries))
	assert.DeepEqual(t, testOtherKey, as.Entries[0].Keys.PublicKeys)
	assert.DeepEqual(t, &kyverno.SecretReference{Name: "testsecret", Namespace: "default"}, as.Entries[1].Keys.Secret)
}

func createStaticKeyAttestorSet(s string, withPublicKey, withSecret, withKMS bool) kyverno.AttestorSet {
	var entries []kyverno.Attestor
	if withPublicKey {
		attestor := kyverno.Attestor{
			Keys: &kyverno.StaticKeyAttestor{
				PublicKeys: s,
			},
		}
		entries = append(entries, attestor)
	}
	if withSecret {
		attestor := kyverno.Attestor{
			Keys: &kyverno.StaticKeyAttestor{
				Secret: &kyverno.SecretReference{
					Name:      "testsecret",
					Namespace: "default",
				},
			},
		}
		entries = append(entries, attestor)
	}
	if withKMS {
		kmsKey := "gcpkms://projects/test_project_id/locations/asia-south1/keyRings/test_key_ring_name/cryptoKeys/test_key_name/versions/1"
		attestor := kyverno.Attestor{
			Keys: &kyverno.StaticKeyAttestor{
				KMS: kmsKey,
			},
		}
		entries = append(entries, attestor)
	}
	return kyverno.AttestorSet{Entries: entries}
}

func Test_ChangedAnnotation(t *testing.T) {
	annotationKey := engineapi.ImageVerifyAnnotationKey
	annotationNew := fmt.Sprintf("\"annotations\": {\"%s\": \"%s\"}", annotationKey, "true")
	newResource := strings.ReplaceAll(testResource, "\"annotations\": {}", annotationNew)

	policyContext := buildContext(t, testPolicyGood, testResource, testResource)

	hasChanged := internal.HasImageVerifiedAnnotationChanged(policyContext, logging.GlobalLogger())
	assert.Equal(t, hasChanged, false)

	policyContext = buildContext(t, testPolicyGood, newResource, testResource)
	hasChanged = internal.HasImageVerifiedAnnotationChanged(policyContext, logging.GlobalLogger())
	assert.Equal(t, hasChanged, true)

	annotationOld := fmt.Sprintf("\"annotations\": {\"%s\": \"%s\"}", annotationKey, "false")
	oldResource := strings.ReplaceAll(testResource, "\"annotations\": {}", annotationOld)

	policyContext = buildContext(t, testPolicyGood, newResource, oldResource)
	hasChanged = internal.HasImageVerifiedAnnotationChanged(policyContext, logging.GlobalLogger())
	assert.Equal(t, hasChanged, true)
}

func Test_MarkImageVerified(t *testing.T) {
	image := "ghcr.io/jimbugwadia/pause2:latest"
	cosign.ClearMock()
	policyContext := buildContext(t, testPolicyGood, testResource, "")
	err := cosign.SetMock(image, attestationPayloads)
	assert.NilError(t, err)

	engineResponse, verifiedImages := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Assert(t, engineResponse != nil)
	assert.Equal(t, len(engineResponse.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResponse.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass)

	assert.Assert(t, verifiedImages != nil)
	assert.Assert(t, verifiedImages.Data != nil)
	assert.Equal(t, len(verifiedImages.Data), 1)
	assert.Equal(t, verifiedImages.IsVerified(image), true)

	patches, err := verifiedImages.Patches(false, logging.GlobalLogger())
	assert.NilError(t, err)
	assert.Equal(t, len(patches), 2)

	resource := testApplyPatches(t, patches)
	patchedAnnotations := resource.GetAnnotations()
	assert.Equal(t, len(patchedAnnotations), 1)

	json := patchedAnnotations[engineapi.ImageVerifyAnnotationKey]
	assert.Assert(t, json != "")

	verified, err := isImageVerified(resource, image, logging.GlobalLogger())
	assert.NilError(t, err)
	assert.Equal(t, verified, true)
}

func testApplyPatches(t *testing.T, patches [][]byte) unstructured.Unstructured {
	patchedResource, err := utils.ApplyPatches([]byte(testResource), patches)
	assert.NilError(t, err)
	assert.Assert(t, patchedResource != nil)

	u := unstructured.Unstructured{}
	err = u.UnmarshalJSON(patchedResource)
	assert.NilError(t, err)
	return u
}

func Test_ParsePEMDelimited(t *testing.T) {
	testPEMPolicy := `{
	    "apiVersion": "kyverno.io/v1",
	    "kind": "Policy",
	    "metadata": {
	       "name": "check-image"
	    },
	    "spec": {
	       "validationFailureAction": "enforce",
	       "background": false,
	       "webhookTimeoutSeconds": 30,
	       "failurePolicy": "Fail",
	       "rules": [
	          {
	             "name": "check-image",
	             "match": {
	                "any": [
	                   {
	                      "resources": {
	                         "kinds": [
	                            "Pod"
	                         ]
	                      }
	                   }
	                ]
	             },
	             "verifyImages": [
	                {
	                   "imageReferences": [
	                      "*"
	                   ],
	                   "attestors": [
	                      {
	                         "count": 1,
	                         "entries": [
	                            {
	                               "keys": {
	                                  "publicKeys": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEfVMHGmFK4OgVqhy36KZ7a3r4R4/o\nCwaCVvXZV4ZULFbkFZ0IodGqKqcVmgycnoj7d8TpKpAUVNF8kKh90ewH3A==\n-----END PUBLIC KEY-----\n-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE0f1W0XigyPFbX8Xq3QmkbL9gDFTf\nRfc8jF7UadBcwKxiyvPSOKZn+igQfXzpNjrwPSZ58JGvF4Fs8BB3fSRP2g==\n-----END PUBLIC KEY-----"
	                               }
	                            }
	                         ]
	                      }
	                   ]
	                }
	             ]
	          }
	       ]
	    }
	 }`

	image := "ghcr.io/jimbugwadia/pause2:latest"
	cosign.ClearMock()
	policyContext := buildContext(t, testPEMPolicy, testResource, "")
	err := cosign.SetMock(image, signaturePayloads)
	assert.NilError(t, err)

	engineResponse, verifiedImages := testVerifyAndPatchImages(context.TODO(), registryclient.NewOrDie(), nil, policyContext, cfg)
	assert.Assert(t, engineResponse != nil)
	assert.Equal(t, len(engineResponse.PolicyResponse.Rules), 1)
	assert.Equal(t, engineResponse.PolicyResponse.Rules[0].Status, engineapi.RuleStatusPass)

	assert.Assert(t, verifiedImages != nil)
	assert.Assert(t, verifiedImages.Data != nil)
	assert.Equal(t, len(verifiedImages.Data), 1)
	assert.Equal(t, verifiedImages.IsVerified(image), true)
}
