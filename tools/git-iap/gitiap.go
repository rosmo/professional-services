package gitiap

/*
   Copyright 2021 Google LLC

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

import (
	"context"

	"golang.org/x/oauth2"
	"google.golang.org/api/impersonate"
)

type GitIAP struct {
	Audience            string
	ServiceAccountEmail string
}

func NewGitIAP(audience string, sa string) *GitIAP {
	return &GitIAP{
		Audience:            audience,
		ServiceAccountEmail: sa,
	}
}

func (gi *GitIAP) GetIAPToken(token *oauth2.Token) (*oauth2.Token, error) {
	ctx := context.Background()

	ts, err := impersonate.IDTokenSource(ctx, impersonate.IDTokenConfig{
		Audience:        gi.Audience,
		TargetPrincipal: gi.ServiceAccountEmail,
		IncludeEmail:    true,
	})
	if err != nil {
		return nil, err
	}

	rts := oauth2.ReuseTokenSource(token, ts)
	result, err := rts.Token()
	if err != nil {
		return nil, err
	}
	return result, nil
}
