// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

func main() {
	currentVersion, err := lookupVersion()
	if err != nil {
		fmt.Println(err)
	}

	var version string
	getVersionCmd := flag.NewFlagSet("get-version", flag.ExitOnError)
	getUnregisteredVersionsCmd := flag.NewFlagSet("get-unregistered-versions", flag.ExitOnError)
	validateRegisteredVersionsCmd := flag.NewFlagSet("validate-registered-versions", flag.ExitOnError)
	registerWasmCmd := flag.NewFlagSet("register-wasm", flag.ExitOnError)
	generateConstantsCmd := flag.NewFlagSet("generate-constants", flag.ExitOnError)
	generateProcessorsUpdateCmd := flag.NewFlagSet("generate-processors-update", flag.ExitOnError)

	addVersionFlag(&version, currentVersion, registerWasmCmd)
	addVersionFlag(&version, currentVersion, generateConstantsCmd)
	addVersionFlag(&version, currentVersion, generateProcessorsUpdateCmd)

	switch os.Args[1] {
	case getVersionCmd.Name():
		version, err := extractProcessorsVersionFromGoModule()
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(version)
		}
	case generateConstantsCmd.Name():
		_ = generateConstantsCmd.Parse(os.Args[2:])
		err = generateVersionsDotGoFile(version)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(version)
		}
	case registerWasmCmd.Name():
		_ = registerWasmCmd.Parse(os.Args[2:])
		err = registerWebAssemblyVersion(version)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(version)
		}
	case generateProcessorsUpdateCmd.Name():
		_ = generateProcessorsUpdateCmd.Parse(os.Args[2:])
		argument, err := generateProcessorsGoGetArgument(version)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(argument)
		}
	case getUnregisteredVersionsCmd.Name():
		releases, err := getNonRegisteredCollectorContribReleases()
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(releases)
		}
	case validateRegisteredVersionsCmd.Name():
		err = validateWebAssemblyVersions()
		if err != nil {
			fmt.Println(err.Error())
		}
	default:
		flag.PrintDefaults()
		os.Exit(1)
	}
}

func addVersionFlag(target *string, defaultVal string, cmd *flag.FlagSet) {
	cmd.StringVar(target, "version", defaultVal, "opentelemetry-collector-contrib version")
}

func lookupVersion() (string, error) {
	version := os.Getenv("PROCESSORS_VERSION")
	if version == "" {
		return extractProcessorsVersionFromGoModule()
	}
	return version, nil
}

func lookupWasmVersionsJSONPath() string {
	path, ok := os.LookupEnv("WASM_VERSIONS_FILE")
	if ok {
		return path
	}
	return filepath.Join("web", "public", "wasm", "versions.json")
}

func extractProcessorsVersionFromGoModule() (string, error) {
	goModFile, err := os.Open("go.mod")
	if err != nil {
		return "", err
	}

	goModFileBytes, err := io.ReadAll(goModFile)
	_ = goModFile.Close()
	if err != nil {
		return "", err
	}

	goModInfo, err := modfile.Parse("go.mod", goModFileBytes, nil)
	if err != nil {
		return "", err
	}

	var version string
	for _, dep := range goModInfo.Require {
		if dep.Indirect {
			continue
		}
		if strings.HasPrefix(dep.Mod.Path, "github.com/open-telemetry/opentelemetry-collector-contrib/processor/") {
			if version != "" && version != dep.Mod.Version {
				return "", fmt.Errorf("multiple opentelemetry-collector-contrib versions found: %q and %q", version, dep.Mod.Version)
			}
			version = dep.Mod.Version
		}
	}

	return version, nil
}

func registerWebAssemblyVersion(version string) error {
	wasmVersionsFilePath := lookupWasmVersionsJSONPath()
	wasmVersionsFile, err := os.OpenFile(wasmVersionsFilePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	wasmVersionsFileBytes, err := io.ReadAll(wasmVersionsFile)
	if err != nil {
		return err
	}

	_ = wasmVersionsFile.Close()

	var wasmVersions map[string]any
	if len(wasmVersionsFileBytes) == 0 {
		wasmVersions = map[string]any{}
	} else {
		err = json.Unmarshal(wasmVersionsFileBytes, &wasmVersions)
		if err != nil {
			return err
		}
	}

	if len(wasmVersions) == 0 {
		wasmVersions["versions"] = []any{}
	}

	for _, v := range wasmVersions["versions"].([]any) {
		if v.(map[string]any)["version"].(string) == version {
			return nil
		}
	}

	wasmName := fmt.Sprintf("wasm/ottlplayground-%s.wasm", version)
	wasmVersions["versions"] = append(wasmVersions["versions"].([]any), map[string]any{
		"artifact": wasmName,
		"version":  version,
	})

	slices.SortFunc(wasmVersions["versions"].([]any), func(a, b any) int {
		return semver.Compare(b.(map[string]any)["version"].(string), a.(map[string]any)["version"].(string))
	})

	modifiedWasmVersions, err := json.MarshalIndent(&wasmVersions, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(wasmVersionsFilePath, modifiedWasmVersions, 0600)
	if err != nil {
		return err
	}

	return nil
}

func validateWebAssemblyVersions() error {
	wasmVersionsFilePath := lookupWasmVersionsJSONPath()
	wasmVersionsFileBytes, err := os.ReadFile(wasmVersionsFilePath)
	if err != nil {
		return fmt.Errorf("file %s not found", wasmVersionsFilePath)
	}

	var wasmVersions map[string]any
	if len(wasmVersionsFileBytes) == 0 {
		return nil
	} else {
		err = json.Unmarshal(wasmVersionsFileBytes, &wasmVersions)
		if err != nil {
			return err
		}
	}

	if len(wasmVersions) == 0 {
		return nil
	}

	var errorsList strings.Builder
	for _, version := range wasmVersions["versions"].([]any) {
		artifact := version.(map[string]any)["artifact"]
		_, err = os.Stat(fmt.Sprintf("web/public/%s", artifact.(string)))
		if err != nil {
			errorsList.WriteString(fmt.Sprintf("version %s: artifact not found: %s \n", version.(map[string]any)["version"].(string), artifact))
		}
	}

	if errorsList.Len() > 0 {
		return errors.New(errorsList.String())
	}

	return nil
}

func generateProcessorsGoGetArgument(version string) (string, error) {
	goModFile, err := os.Open("go.mod")
	if err != nil {
		return "", err
	}

	defer func(goModFile *os.File) {
		_ = goModFile.Close()
	}(goModFile)

	goModFileBytes, err := io.ReadAll(goModFile)
	if err != nil {
		return "", err
	}

	goModInfo, err := modfile.Parse("go.mod", goModFileBytes, nil)
	if err != nil {
		return "", err
	}

	var argument strings.Builder
	seem := map[string]bool{}
	for _, dep := range goModInfo.Require {
		if dep.Indirect {
			continue
		}
		if !seem[dep.Mod.Path] && strings.HasPrefix(dep.Mod.Path, "github.com/open-telemetry/opentelemetry-collector-contrib/processor/") {
			seem[dep.Mod.Path] = true
			argument.WriteString(fmt.Sprintf("%s@%s", dep.Mod.Path, version))
			argument.WriteString(" ")
		}
	}

	// Additional collector dependencies
	argument.WriteString("go.opentelemetry.io/collector/consumer@" + version)
	argument.WriteString(" ")
	argument.WriteString("go.opentelemetry.io/collector/processor@" + version)
	argument.WriteString(" ")
	argument.WriteString("go.opentelemetry.io/collector/component@" + version)

	return argument.String(), nil
}

func generateVersionsDotGoFile(version string) error {
	versionsGoFile, err := os.Create(filepath.Join("internal", "versions.go"))
	if err != nil {
		return err
	}

	defer func(versionsGoFile *os.File) {
		_ = versionsGoFile.Close()
	}(versionsGoFile)

	content := strings.Builder{}
	content.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
	content.WriteString("// Code generated. DO NOT EDIT.\n\n")
	content.WriteString("package internal\n\n")
	content.WriteString(fmt.Sprintf("const CollectorContribProcessorsVersion = \"%s\" \n", version))

	formattedSource, err := format.Source([]byte(content.String()))
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(versionsGoFile, string(formattedSource))
	if err != nil {
		return err
	}

	return nil
}

func getNonRegisteredCollectorContribReleases() (string, error) {
	wasmVersionsFilePath := lookupWasmVersionsJSONPath()
	registeredVersions := map[string]bool{}

	_, err := os.Stat(wasmVersionsFilePath)
	if err == nil {
		var wasmVersionsFileBytes []byte
		wasmVersionsFileBytes, err = os.ReadFile(wasmVersionsFilePath)
		if err != nil {
			return "", err
		}
		if len(wasmVersionsFileBytes) > 0 {
			var fileContent map[string]any
			err = json.Unmarshal(wasmVersionsFileBytes, &fileContent)
			if err != nil {
				return "", err
			}
			for _, v := range fileContent["versions"].([]any) {
				registeredVersions[v.(map[string]any)["version"].(string)] = true
			}
		}
	}

	tagsRes, err := http.Get("https://api.github.com/repos/open-telemetry/opentelemetry-collector-contrib/releases")
	if err != nil {
		return "", err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(tagsRes.Body)

	if tagsRes.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get release list. status: %s", tagsRes.Status)
	}

	var data []map[string]any
	err = json.NewDecoder(tagsRes.Body).Decode(&data)
	if err != nil {
		return "", err
	}

	var newVersions []string
	for _, release := range data {
		// versions <= v0.110.0 fails to compile due to some breaking changes
		if !registeredVersions[release["name"].(string)] && semver.Compare(release["name"].(string), "v0.110.0") > 0 {
			newVersions = append(newVersions, release["name"].(string))
		}
	}

	slices.SortFunc(newVersions, semver.Compare)
	return strings.Join(newVersions, " "), nil
}