// Package vrclib holds the files that are built into the program: the window's pages, the Unity packages
// the program installs into projects, and the list of changes.
package vrclib

import "embed"

// WebFS: the pages shown in the window (web/).
//
//go:embed web/*
var WebFS embed.FS

// UnityHelper: the program's own Unity packages (unityhelper/).
//
//go:embed unityhelper
var UnityHelper embed.FS

// UnityKit: UnitySkills, shipped with the program (unitykit/).
//
//go:embed unitykit
var UnityKit embed.FS

// ChangelogMD: CHANGELOG.md, shown after an update.
//
//go:embed CHANGELOG.md
var ChangelogMD string
