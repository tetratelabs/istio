// Copyright Istio Authors
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

package markdown

import "fmt"

func InlineCode(code string) string {
	return fmt.Sprintf("`%s`", code)
}

func CodeBlock(lang string, indent string, code string) string {
	return fmt.Sprintf("%[1]s```%[2]s\n%[3]s\n%[1]s```", indent, lang, code)
}
