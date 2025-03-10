package engine

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov1beta1 "github.com/kyverno/kyverno/api/kyverno/v1beta1"
	"github.com/kyverno/kyverno/cmd/cli/kubectl-kyverno/utils/store"
	"github.com/kyverno/kyverno/pkg/engine/context"
	datautils "github.com/kyverno/kyverno/pkg/utils/data"
	matchutils "github.com/kyverno/kyverno/pkg/utils/match"
	"github.com/kyverno/kyverno/pkg/utils/wildcard"
	"golang.org/x/exp/slices"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EngineStats stores in the statistics for a single application of resource
type EngineStats struct {
	// average time required to process the policy rules on a resource
	ExecutionTime time.Duration
	// Count of rules that were applied successfully
	RulesAppliedCount int
}

func checkNameSpace(namespaces []string, resource unstructured.Unstructured) bool {
	resourceNameSpace := resource.GetNamespace()
	if resource.GetKind() == "Namespace" {
		resourceNameSpace = resource.GetName()
	}

	for _, namespace := range namespaces {
		if wildcard.Match(namespace, resourceNameSpace) {
			return true
		}
	}

	return false
}

// doesResourceMatchConditionBlock filters the resource with defined conditions
// for a match / exclude block, it has the following attributes:
// ResourceDescription:
//
//	Kinds      []string
//	Name       string
//	Namespaces []string
//	Selector
//
// UserInfo:
//
//	Roles        []string
//	ClusterRoles []string
//	Subjects     []rbacv1.Subject
//
// To filter out the targeted resources with ResourceDescription, the check
// should be: AND across attributes but an OR inside attributes that of type list
// To filter out the targeted resources with UserInfo, the check
// should be: OR (across & inside) attributes
func doesResourceMatchConditionBlock(subresourceGVKToAPIResource map[string]*metav1.APIResource, conditionBlock kyvernov1.ResourceDescription, userInfo kyvernov1.UserInfo, admissionInfo kyvernov1beta1.RequestInfo, resource unstructured.Unstructured, dynamicConfig []string, namespaceLabels map[string]string, subresourceInAdmnReview string) []error {
	var errs []error

	if len(conditionBlock.Kinds) > 0 {
		// Matching on ephemeralcontainers even when they are not explicitly specified for backward compatibility.
		if !matchutils.CheckKind(subresourceGVKToAPIResource, conditionBlock.Kinds, resource.GroupVersionKind(), subresourceInAdmnReview, true) {
			errs = append(errs, fmt.Errorf("kind does not match %v", conditionBlock.Kinds))
		}
	}

	resourceName := resource.GetName()
	if resourceName == "" {
		resourceName = resource.GetGenerateName()
	}

	if conditionBlock.Name != "" {
		if !matchutils.CheckName(conditionBlock.Name, resourceName) {
			errs = append(errs, fmt.Errorf("name does not match"))
		}
	}

	if len(conditionBlock.Names) > 0 {
		noneMatch := true
		for i := range conditionBlock.Names {
			if matchutils.CheckName(conditionBlock.Names[i], resourceName) {
				noneMatch = false
				break
			}
		}
		if noneMatch {
			errs = append(errs, fmt.Errorf("none of the names match"))
		}
	}

	if len(conditionBlock.Namespaces) > 0 {
		if !checkNameSpace(conditionBlock.Namespaces, resource) {
			errs = append(errs, fmt.Errorf("namespace does not match"))
		}
	}

	if len(conditionBlock.Annotations) > 0 {
		if !matchutils.CheckAnnotations(conditionBlock.Annotations, resource.GetAnnotations()) {
			errs = append(errs, fmt.Errorf("annotations does not match"))
		}
	}

	if conditionBlock.Selector != nil {
		hasPassed, err := matchutils.CheckSelector(conditionBlock.Selector, resource.GetLabels())
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to parse selector: %v", err))
		} else {
			if !hasPassed {
				errs = append(errs, fmt.Errorf("selector does not match"))
			}
		}
	}

	if conditionBlock.NamespaceSelector != nil && resource.GetKind() != "Namespace" &&
		(resource.GetKind() != "" || slices.Contains(conditionBlock.Kinds, "*") && wildcard.Match("*", resource.GetKind())) {
		hasPassed, err := matchutils.CheckSelector(conditionBlock.NamespaceSelector, namespaceLabels)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to parse namespace selector: %v", err))
		} else {
			if !hasPassed {
				errs = append(errs, fmt.Errorf("namespace selector does not match"))
			}
		}
	}

	keys := append(admissionInfo.AdmissionUserInfo.Groups, admissionInfo.AdmissionUserInfo.Username)
	var userInfoErrors []error
	if len(userInfo.Roles) > 0 && !datautils.SliceContains(keys, dynamicConfig...) {
		if !datautils.SliceContains(userInfo.Roles, admissionInfo.Roles...) {
			userInfoErrors = append(userInfoErrors, fmt.Errorf("user info does not match roles for the given conditionBlock"))
		}
	}

	if len(userInfo.ClusterRoles) > 0 && !datautils.SliceContains(keys, dynamicConfig...) {
		if !datautils.SliceContains(userInfo.ClusterRoles, admissionInfo.ClusterRoles...) {
			userInfoErrors = append(userInfoErrors, fmt.Errorf("user info does not match clustersRoles for the given conditionBlock"))
		}
	}

	if len(userInfo.Subjects) > 0 {
		if !matchSubjects(userInfo.Subjects, admissionInfo.AdmissionUserInfo, dynamicConfig) {
			userInfoErrors = append(userInfoErrors, fmt.Errorf("user info does not match subject for the given conditionBlock"))
		}
	}
	return append(errs, userInfoErrors...)
}

// matchSubjects return true if one of ruleSubjects exist in userInfo
func matchSubjects(ruleSubjects []rbacv1.Subject, userInfo authenticationv1.UserInfo, dynamicConfig []string) bool {
	if store.IsMock() {
		mockSubject := store.GetSubject()
		for _, subject := range ruleSubjects {
			switch subject.Kind {
			case "ServiceAccount":
				if subject.Name == mockSubject.Name && subject.Namespace == mockSubject.Namespace {
					return true
				}
			case "User", "Group":
				if mockSubject.Name == subject.Name {
					return true
				}
			}
		}
		return false
	} else {
		return matchutils.CheckSubjects(ruleSubjects, userInfo, dynamicConfig)
	}
}

// MatchesResourceDescription checks if the resource matches resource description of the rule or not
func MatchesResourceDescription(subresourceGVKToAPIResource map[string]*metav1.APIResource, resourceRef unstructured.Unstructured, ruleRef kyvernov1.Rule, admissionInfoRef kyvernov1beta1.RequestInfo, dynamicConfig []string, namespaceLabels map[string]string, policyNamespace, subresourceInAdmnReview string) error {
	rule := ruleRef.DeepCopy()
	resource := *resourceRef.DeepCopy()
	admissionInfo := *admissionInfoRef.DeepCopy()
	empty := []string{}

	var reasonsForFailure []error
	if policyNamespace != "" && policyNamespace != resourceRef.GetNamespace() {
		return fmt.Errorf(" The policy and resource namespace are different. Therefore, policy skip this resource.")
	}

	if len(rule.MatchResources.Any) > 0 {
		// include object if ANY of the criteria match
		// so if one matches then break from loop
		oneMatched := false
		for _, rmr := range rule.MatchResources.Any {
			// if there are no errors it means it was a match
			if len(matchesResourceDescriptionMatchHelper(subresourceGVKToAPIResource, rmr, admissionInfo, resource, empty, namespaceLabels, subresourceInAdmnReview)) == 0 {
				oneMatched = true
				break
			}
		}
		if !oneMatched {
			reasonsForFailure = append(reasonsForFailure, fmt.Errorf("no resource matched"))
		}
	} else if len(rule.MatchResources.All) > 0 {
		// include object if ALL of the criteria match
		for _, rmr := range rule.MatchResources.All {
			reasonsForFailure = append(reasonsForFailure, matchesResourceDescriptionMatchHelper(subresourceGVKToAPIResource, rmr, admissionInfo, resource, empty, namespaceLabels, subresourceInAdmnReview)...)
		}
	} else {
		rmr := kyvernov1.ResourceFilter{UserInfo: rule.MatchResources.UserInfo, ResourceDescription: rule.MatchResources.ResourceDescription}
		reasonsForFailure = append(reasonsForFailure, matchesResourceDescriptionMatchHelper(subresourceGVKToAPIResource, rmr, admissionInfo, resource, empty, namespaceLabels, subresourceInAdmnReview)...)
	}

	if len(rule.ExcludeResources.Any) > 0 {
		// exclude the object if ANY of the criteria match
		for _, rer := range rule.ExcludeResources.Any {
			reasonsForFailure = append(reasonsForFailure, matchesResourceDescriptionExcludeHelper(subresourceGVKToAPIResource, rer, admissionInfo, resource, dynamicConfig, namespaceLabels, subresourceInAdmnReview)...)
		}
	} else if len(rule.ExcludeResources.All) > 0 {
		// exclude the object if ALL the criteria match
		excludedByAll := true
		for _, rer := range rule.ExcludeResources.All {
			// we got no errors inplying a resource did NOT exclude it
			// "matchesResourceDescriptionExcludeHelper" returns errors if resource is excluded by a filter
			if len(matchesResourceDescriptionExcludeHelper(subresourceGVKToAPIResource, rer, admissionInfo, resource, dynamicConfig, namespaceLabels, subresourceInAdmnReview)) == 0 {
				excludedByAll = false
				break
			}
		}
		if excludedByAll {
			reasonsForFailure = append(reasonsForFailure, fmt.Errorf("resource excluded since the combination of all criteria exclude it"))
		}
	} else {
		rer := kyvernov1.ResourceFilter{UserInfo: rule.ExcludeResources.UserInfo, ResourceDescription: rule.ExcludeResources.ResourceDescription}
		reasonsForFailure = append(reasonsForFailure, matchesResourceDescriptionExcludeHelper(subresourceGVKToAPIResource, rer, admissionInfo, resource, dynamicConfig, namespaceLabels, subresourceInAdmnReview)...)
	}

	// creating final error
	errorMessage := fmt.Sprintf("rule %s not matched:", ruleRef.Name)
	for i, reasonForFailure := range reasonsForFailure {
		if reasonForFailure != nil {
			errorMessage += "\n " + fmt.Sprint(i+1) + ". " + reasonForFailure.Error()
		}
	}

	if len(reasonsForFailure) > 0 {
		return fmt.Errorf(errorMessage)
	}

	return nil
}

func matchesResourceDescriptionMatchHelper(subresourceGVKToAPIResource map[string]*metav1.APIResource, rmr kyvernov1.ResourceFilter, admissionInfo kyvernov1beta1.RequestInfo, resource unstructured.Unstructured, dynamicConfig []string, namespaceLabels map[string]string, subresourceInAdmnReview string) []error {
	var errs []error
	if reflect.DeepEqual(admissionInfo, kyvernov1beta1.RequestInfo{}) {
		rmr.UserInfo = kyvernov1.UserInfo{}
	}

	// checking if resource matches the rule
	if !reflect.DeepEqual(rmr.ResourceDescription, kyvernov1.ResourceDescription{}) ||
		!reflect.DeepEqual(rmr.UserInfo, kyvernov1.UserInfo{}) {
		matchErrs := doesResourceMatchConditionBlock(subresourceGVKToAPIResource, rmr.ResourceDescription, rmr.UserInfo, admissionInfo, resource, dynamicConfig, namespaceLabels, subresourceInAdmnReview)
		errs = append(errs, matchErrs...)
	} else {
		errs = append(errs, fmt.Errorf("match cannot be empty"))
	}
	return errs
}

func matchesResourceDescriptionExcludeHelper(subresourceGVKToAPIResource map[string]*metav1.APIResource, rer kyvernov1.ResourceFilter, admissionInfo kyvernov1beta1.RequestInfo, resource unstructured.Unstructured, dynamicConfig []string, namespaceLabels map[string]string, subresourceInAdmnReview string) []error {
	var errs []error
	// checking if resource matches the rule
	if !reflect.DeepEqual(rer.ResourceDescription, kyvernov1.ResourceDescription{}) ||
		!reflect.DeepEqual(rer.UserInfo, kyvernov1.UserInfo{}) {
		excludeErrs := doesResourceMatchConditionBlock(subresourceGVKToAPIResource, rer.ResourceDescription, rer.UserInfo, admissionInfo, resource, dynamicConfig, namespaceLabels, subresourceInAdmnReview)
		// it was a match so we want to exclude it
		if len(excludeErrs) == 0 {
			errs = append(errs, fmt.Errorf("resource excluded since one of the criteria excluded it"))
			errs = append(errs, excludeErrs...)
		}
	}
	// len(errs) != 0 if the filter excluded the resource
	return errs
}

// excludeResource checks if the resource has ownerRef set
func excludeResource(podControllers string, resource unstructured.Unstructured) bool {
	kind := resource.GetKind()
	hasOwner := false
	if kind == "Pod" || kind == "Job" {
		for _, owner := range resource.GetOwnerReferences() {
			hasOwner = true
			if owner.Kind != "ReplicaSet" && !strings.Contains(podControllers, owner.Kind) {
				return false
			}
		}
		return hasOwner
	}

	return false
}

// ManagedPodResource returns true:
// - if the policy has auto-gen annotation && resource == Pod
// - if the auto-gen contains cronJob && resource == Job
func ManagedPodResource(policy kyvernov1.PolicyInterface, resource unstructured.Unstructured) bool {
	podControllers, ok := policy.GetAnnotations()[kyvernov1.PodControllersAnnotation]
	if !ok || strings.ToLower(podControllers) == "none" {
		return false
	}

	if excludeResource(podControllers, resource) {
		return true
	}

	if strings.Contains(podControllers, "CronJob") && excludeResource(podControllers, resource) {
		return true
	}

	return false
}

func evaluateList(jmesPath string, ctx context.EvalInterface) ([]interface{}, error) {
	i, err := ctx.Query(jmesPath)
	if err != nil {
		return nil, err
	}

	l, ok := i.([]interface{})
	if !ok {
		return []interface{}{i}, nil
	}

	return l, nil
}

// invertedElement inverted the order of element for patchStrategicMerge  policies as kustomize patch revering the order of patch resources.
func invertedElement(elements []interface{}) {
	for i, j := 0, len(elements)-1; i < j; i, j = i+1, j-1 {
		elements[i], elements[j] = elements[j], elements[i]
	}
}
