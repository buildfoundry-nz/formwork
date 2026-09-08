package engine_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
)

func TestDartCommentProjectionThroughEngine(t *testing.T) {
	cases := []struct {
		name, src string
		line      int
	}{
		{"code", "// explanation\nFORBIDDEN();\n", 2},
		{"comment", "// FORBIDDEN();\n", 0},
		{"literal", "// explanation\n'FORBIDDEN';\n", 2},
		{"interpolation", "// explanation\n'${'//'.length}${FORBIDDEN()}';\n", 2},
		{"interpolation-comment", "'${(/* FORBIDDEN */ 3)}';\n", 0},
		{"raw-literal", "// explanation\nr'${/* FORBIDDEN */}';\n", 2},
		{"nested-comment", "/* outer /* FORBIDDEN */ still FORBIDDEN */\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checker := checkerWithParams(t, "forbidden-pattern", "pattern: FORBIDDEN\n")
			rule, err := config.New("dart-comment-probe", "forbidden-pattern", finding.SeverityError,
				"remove the forbidden operation", []string{"**/*.dart"}, nil, nil, checker)
			if err != nil {
				t.Fatal(err)
			}
			rule.Preprocess = "decomment-dart"
			got, err := engine.Run([]*config.Rule{rule}, memFileSet(map[string]string{"lib/pan.dart": tc.src}), 2)
			if err != nil {
				t.Fatal(err)
			}
			if tc.line == 0 {
				if len(got) != 0 {
					t.Fatalf("comment-only case produced findings: %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Path != "lib/pan.dart" || got[0].Line != tc.line {
				t.Fatalf("want one finding at lib/pan.dart:%d, got %+v", tc.line, got)
			}
		})
	}
}
