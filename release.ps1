<#
.SYNOPSIS
Release script for the markdownweb Go module on GitHub using gh CLI.
A module release is a tag and release notes; there are no binaries to upload.

.DESCRIPTION
The release is made from the commit HEAD (workspace DR-0012). The script commits
and pushes nothing: it stops unless tracked files are committed and HEAD is the
tip of its branch on origin, and it creates the draft with --target HEAD. GitHub
makes the tag, on that commit, when you finalize the draft.

Running it again after an interruption is safe (workspace DR-0013). A draft for
the tag that already exists and targets HEAD is complete, since a module release
has no files to upload: the script creates nothing and says to finalize it. A
published release, or a draft for another commit, stops the script.

.PARAMETER DryRun
Do everything up to the prompt (notes), say what would be created and from which
commit, and change nothing.
#>
param(
    [switch]$DryRun
)

function Invoke-Release {
    # Determine repository and group IDs
    $repoId = Split-Path -Leaf -Path (Get-Location)
    $groupId = (git config --get remote.origin.url) -replace '.*github\.com[:/]([^/]+)/.*', '$1'
    $repoUrl = "https://github.com/${groupId}/${repoId}"
    Write-Host "REPO_URL -> ${repoUrl}"

    # Generate release tag and notes
    $releaseTag = "v$(jq -r .version codemeta.json)"
    if ($releaseTag -notmatch '^v[0-9a-zA-Z._-]+$') {
        Write-Error "error: version contains unexpected characters: ${releaseTag}"
        return 1
    }
    Write-Host "tag: ${releaseTag}, notes:"
    jq -r .releaseNotes codemeta.json | Out-File -FilePath release_notes.tmp -Encoding utf8
    Get-Content release_notes.tmp | Out-Host

    # The release is built from one known commit: tracked files are committed
    # and HEAD is the tip of its branch on origin (DR-0012). Done with git, since
    # Windows may not have make.
    $failed = $false
    if (git status --porcelain --untracked-files=no) {
        Write-Error "check-committed: tracked files have uncommitted changes (commit and push them)"
        $failed = $true
    }
    $branch = git branch --show-current
    $releaseCommit = git rev-parse HEAD
    $remoteTip = ((git ls-remote origin "refs/heads/${branch}" 2>$null) -split '\s+' | Select-Object -First 1)
    if (-not $remoteTip) {
        Write-Error "check-committed: cannot find branch ${branch} on origin (push it)"
        $failed = $true
    } elseif ($remoteTip -ne $releaseCommit) {
        Write-Error "check-committed: HEAD is not the tip of origin/${branch} (push, or pull first)"
        $failed = $true
    }
    if ($failed) { return 1 }
    Write-Host "check-committed: $($releaseCommit.Substring(0, 7)) is committed and pushed"

    # A release that already exists is a draft for this commit or it stops the
    # run, so a second release is never created and a published one is never touched.
    $viewJson = gh release view "${releaseTag}" --json isDraft,targetCommitish 2>$null
    if ($LASTEXITCODE -eq 0) {
        $view = ($viewJson -join "`n") | ConvertFrom-Json
        if (-not $view.isDraft) {
            Write-Error "error: ${releaseTag} is not a draft on GitHub; it is published and is not touched"
            return 1
        }
        if ($view.targetCommitish -ne $releaseCommit) {
            Write-Error "error: the draft for ${releaseTag} targets $($view.targetCommitish), not HEAD ${releaseCommit}; delete the draft first"
            return 1
        }
        Write-Host "The draft ${releaseTag} already exists and targets ${releaseCommit}; nothing to create or upload."
        Write-Host "Now finalize the draft: ${repoUrl}/releases"
        return 0
    }

    Write-Host "New release: draft ${releaseTag} at ${releaseCommit} (no files to upload)"
    if ($DryRun) {
        Write-Host "dry run: nothing changed"
        return 0
    }
    $yesNo = Read-Host -Prompt "Create the draft release on GitHub with gh? (y/N)"
    if ($yesNo -ne "y") { return 0 }

    Write-Host "Pushing release ${releaseTag} to GitHub"
    gh release create "${releaseTag}" `
        --draft `
        --target "${releaseCommit}" `
        --notes-file release_notes.tmp `
        --generate-notes | Out-Host
    if ($LASTEXITCODE -ne 0) { return 1 }

    Write-Host @"

Now go to repo release and finalize draft.

    ${repoUrl}/releases

"@
    return 0
}

# The notes file is removed however the script ends.
try {
    # Anything a command leaves on the output stream joins the return value, so take the last item.
    $code = @(Invoke-Release)[-1]
} finally {
    Remove-Item -ErrorAction SilentlyContinue release_notes.tmp
}
exit $code
