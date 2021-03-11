/*
Copyright 2020 The Skaffold Authors

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

package errors

import (
	"errors"
	"fmt"
	"strings"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/constants"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/instrumentation"
	"github.com/GoogleContainerTools/skaffold/proto/v1"
)

const (
	// These are phases in a Skaffolld
	Init        = Phase("Init")
	Build       = Phase("Build")
	Deploy      = Phase("Deploy")
	StatusCheck = Phase("StatusCheck")
	FileSync    = Phase("FileSync")
	DevInit     = Phase("DevInit")
	Cleanup     = Phase("Cleanup")

	// Report issue text
	reportIssueText = "If above error is unexpected, please open an issue " + constants.GithubIssueLink + " to report this error"
)

var (
	reportIssueSuggestion = func(_ Config) []*proto.Suggestion {
		return []*proto.Suggestion{{
			SuggestionCode: proto.SuggestionCode_OPEN_ISSUE,
			Action:         reportIssueText,
		}}
	}
)

type Phase string

// ActionableErr returns an actionable error message with suggestions
func ActionableErr(cfg Config, phase Phase, err error) *proto.ActionableErr {
	errCode, suggestions := getErrorCodeFromError(cfg, phase, err)
	return &proto.ActionableErr{
		ErrCode:     errCode,
		Message:     err.Error(),
		Suggestions: suggestions,
	}
}

func ShowAIError(cfg Config, err error) error {
	if IsSkaffoldErr(err) {
		instrumentation.SetErrorCode(err.(Error).StatusCode())
		return err
	}

	var knownProblems = append(knownBuildProblems, knownDeployProblems...)
	for _, v := range append(knownProblems, knownInitProblems...) {
		if v.regexp.MatchString(err.Error()) {
			instrumentation.SetErrorCode(v.errCode)
			if suggestions := v.suggestion(cfg); suggestions != nil {
				description := fmt.Sprintf("%s\n", err)
				if v.description != nil {
					description = strings.Trim(v.description(err), ".")
				}
				return fmt.Errorf("%s. %s", description, concatSuggestions(suggestions))
			}
			return fmt.Errorf(v.description(err))
		}
	}
	return err
}

func IsOldImageManifestProblem(cfg Config, err error) (string, bool) {
	if err != nil && oldImageManifest.regexp.MatchString(err.Error()) {
		if s := oldImageManifest.suggestion(cfg); s != nil {
			return fmt.Sprintf("%s. %s", oldImageManifest.description(err),
				concatSuggestions(s)), true
		}
		return "", true
	}
	return "", false
}

func getErrorCodeFromError(cfg Config, phase Phase, err error) (proto.StatusCode, []*proto.Suggestion) {
	var sErr Error
	if errors.As(err, &sErr) {
		return sErr.StatusCode(), sErr.Suggestions()
	}

	if problems, ok := allErrors[phase]; ok {
		for _, v := range problems {
			if v.regexp.MatchString(err.Error()) {
				return v.errCode, v.suggestion(cfg)
			}
		}
	}
	return proto.StatusCode_UNKNOWN_ERROR, nil
}

func concatSuggestions(suggestions []*proto.Suggestion) string {
	var s strings.Builder
	for _, suggestion := range suggestions {
		if s.String() != "" {
			s.WriteString(" or ")
		}
		s.WriteString(suggestion.Action)
	}
	if s.String() == "" {
		return ""
	}
	s.WriteString(".")
	return s.String()
}

var allErrors = map[Phase][]problem{
	Build: append(knownBuildProblems, problem{
		regexp:     re(".*"),
		errCode:    proto.StatusCode_BUILD_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}),
	Init: append(knownInitProblems, problem{
		regexp:     re(".*"),
		errCode:    proto.StatusCode_INIT_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}),
	Deploy: append(knownDeployProblems, problem{
		regexp:     re(".*"),
		errCode:    proto.StatusCode_DEPLOY_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}),
	StatusCheck: {{
		regexp:     re(".*"),
		errCode:    proto.StatusCode_STATUSCHECK_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}},
	FileSync: {{
		regexp:     re(".*"),
		errCode:    proto.StatusCode_SYNC_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}},
	DevInit: {oldImageManifest, {
		regexp:     re(".*"),
		errCode:    proto.StatusCode_DEVINIT_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}},
	Cleanup: {{
		regexp:     re(".*"),
		errCode:    proto.StatusCode_CLEANUP_UNKNOWN,
		suggestion: reportIssueSuggestion,
	}},
}
