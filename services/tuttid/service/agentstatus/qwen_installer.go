package agentstatus

import (
	"context"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// QwenCodeNPMInstallPackage is the npm package name that ships the Qwen Code
// CLI. Mirrors how Codex is installed (`@openai/codex`) but for the Alibaba
// distribution. The `qwen` binary it ships is the same one users run
// interactively; `qwen serve` is the ACP daemon Tutti integrates against.
//
// Pinned as a constant so the install command line, the wizard display
// string, and the package.json walker (resolveAdapterPackageVersion) all
// agree without string-literal drift.
const QwenCodeNPMInstallPackage = "@qwen-code/qwen-code"

// QwenCodeBinaryName is the CLI launcher name after `npm install -g`. The
// daemon is launched via `qwen serve`, but every detection / install probe
// uses `qwen` so a half-installed platform package still resolves.
const QwenCodeBinaryName = "qwen"

// runQwenCodeLatestInstaller installs (or repairs in place) the qwen-code npm
// package via the same managed-npm path used for @openai/codex. The actual
// npm invocation lives in runManagedNPMPackageInstaller; this wrapper just
// pins the package + binary so the agentstatus registry can dispatch to it
// without leaking provider-specific literals into the registry.
//
// existingCLIPath is the path of any previously-resolved `qwen` binary. If
// it lives in a version-manager prefix that already owns a (possibly broken)
// copy of @qwen-code/qwen-code, runManagedNPMPackageInstaller will repair
// that prefix instead of installing a second copy in ~/.local — same
// behavior as codex, to avoid the "platform package incomplete" loop.
func (s Service) runQwenCodeLatestInstaller(
	ctx context.Context,
	spec InstallerSpec,
	existingCLIPath string,
) (InstallCommandResult, error) {
	if spec.ManagedNPM == nil {
		return InstallCommandResult{
			ExitCode: 1,
			Stderr:   "qwen-code managed npm installer spec is required",
		}, nil
	}
	return s.runManagedNPMPackageInstaller(ctx, agentprovider.QwenCode, ManagedNPMPackageInstallerSpec{
		PackageName:     QwenCodeNPMInstallPackage,
		BinaryName:      QwenCodeBinaryName,
		IncludeOptional: true,
	}, existingCLIPath)
}

// qwenCodeInstallerSpec returns the InstallerSpec used by the agentstatus
// registry for the qwen-code provider. Kind=InstallerKindManagedNPMPackage
// routes through runManagedNPMPackageInstaller above, which is the same
// path codex uses — that keeps the npm-cache / repair / registry-rotation
// logic shared between the two providers.
func qwenCodeInstallerSpec() InstallerSpec {
	return InstallerSpec{
		Kind:           InstallerKindManagedNPMPackage,
		DisplayCommand: "npm install -g " + QwenCodeNPMInstallPackage + " --include=optional",
		ManagedNPM: &ManagedNPMPackageInstallerSpec{
			PackageName:     QwenCodeNPMInstallPackage,
			BinaryName:      QwenCodeBinaryName,
			IncludeOptional: true,
		},
	}
}