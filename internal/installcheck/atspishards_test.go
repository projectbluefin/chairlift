package installcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// shardExpression is the only tag-expression shape a shard may use: feature
// tags joined by "or". "and" or "not" would select scenarios by something
// other than the feature they belong to, which this gate cannot account for.
var shardExpression = regexp.MustCompile(`^@[A-Za-z0-9_-]+( or @[A-Za-z0-9_-]+)*$`)

// TestATSPIShardsCoverEveryFeatureOnce holds test.yml's AT-SPI shard matrix
// to the feature files: every feature's first tag appears in exactly one
// shard, and no shard names a feature that does not exist. A feature in no
// shard would silently stop running on every pull request; a feature in two
// would run twice and skew the balance the shards are chosen for.
//
// Behave selects scenarios by their effective tags — the feature's tags plus
// the scenario's own — so a feature's first tag must come from the tag block
// above its Feature: line, and must not appear on any other feature's
// scenarios: either would put a scenario in a shard its feature is not in.
func TestATSPIShardsCoverEveryFeatureOnce(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						Shard string `yaml:"shard"`
						Tags  string `yaml:"tags"`
					} `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	path := filepath.Join(".github", "workflows", "test.yml")
	if err := yaml.Unmarshal([]byte(readRepoFile(t, path)), &workflow); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	shards := workflow.Jobs["atspi"].Strategy.Matrix.Include
	if len(shards) == 0 {
		t.Fatalf("%s has no atspi shard matrix", path)
	}

	features := map[string]string{}
	paths, err := filepath.Glob(filepath.Join(RepoRoot(), "test", "e2e", "features", "*.feature"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no feature files found: %v", err)
	}
	type occurrence struct{ file, tag string }
	var tagged []occurrence
	for _, file := range paths {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(file)
		tag, header := "", true
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Feature:") {
				header = false
				continue
			}
			if !strings.HasPrefix(line, "@") {
				continue
			}
			fields := strings.Fields(line)
			if header && tag == "" {
				tag, fields = fields[0], fields[1:]
			}
			for _, field := range fields {
				tagged = append(tagged, occurrence{name, field})
			}
		}
		if tag == "" {
			t.Errorf("%s has no feature tag above its Feature: line to shard it by", name)
			continue
		}
		if previous, dup := features[tag]; dup {
			t.Errorf("%s and %s share the feature tag %s, so no shard can select one without the other", previous, name, tag)
		}
		features[tag] = name
	}
	for _, o := range tagged {
		if owner, ok := features[o.tag]; ok && owner != o.file {
			t.Errorf("%s tags a scenario %s, %s's feature tag, so that scenario also runs in %s's shard", o.file, o.tag, owner, owner)
		}
	}

	seen := map[string]string{}
	for _, shard := range shards {
		if !shardExpression.MatchString(shard.Tags) {
			t.Errorf("shard %q tags %q are not feature tags joined by \"or\"", shard.Shard, shard.Tags)
			continue
		}
		for _, tag := range strings.Split(shard.Tags, " or ") {
			if _, ok := features[tag]; !ok {
				t.Errorf("shard %q names %s, which no feature file carries", shard.Shard, tag)
			}
			if previous, dup := seen[tag]; dup {
				t.Errorf("%s runs in both shard %q and shard %q", tag, previous, shard.Shard)
			}
			seen[tag] = shard.Shard
		}
	}
	var missing []string
	for tag, file := range features {
		if _, ok := seen[tag]; !ok {
			missing = append(missing, tag+" ("+file+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("features in no AT-SPI shard, so no pull request runs them: %s", strings.Join(missing, ", "))
	}
}
