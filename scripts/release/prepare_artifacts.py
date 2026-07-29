#!/usr/bin/env python3
"""Build and package the sealed local application and Pages release assets."""

from __future__ import annotations

import gzip
import hashlib
import io
import json
import os
import platform
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence

VERSION_PATTERN = re.compile(
    r"^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)"
    r"(?:[-+][0-9A-Za-z.-]+)?$"
)
APPLICATION_NAME = "download-your-data"
APPLICATION_PLATFORM = "darwin"
APPLICATION_ARCHITECTURE = "arm64"
APPLICATION_DOCUMENTS = {
    "README.md": "README.md",
    "FIRST_RUN.md": "docs/first-run.md",
    "LICENSE": "LICENSE",
}
PAGES_FILES = ("index.html", "styles.css")


class ArtifactError(RuntimeError):
    """A release artifact boundary rejected its input or output."""


@dataclass(frozen=True)
class ReleaseContext:
    repository_root: Path
    artifact_directory: Path
    version: str
    source_commit: str
    release_timestamp: str
    source_epoch: int

    @property
    def asset_directory(self) -> Path:
        return self.artifact_directory / "payloads" / "release-assets"


def run_checked(
    arguments: Sequence[str],
    *,
    cwd: Path,
    environment: Mapping[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    completed = subprocess.run(
        list(arguments),
        cwd=cwd,
        env=dict(environment) if environment is not None else None,
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if completed.returncode != 0:
        command = " ".join(arguments)
        raise ArtifactError(
            f"command failed ({completed.returncode}): {command}\n"
            f"{completed.stdout}{completed.stderr}"
        )
    return completed


def required_environment(name: str) -> str:
    value = os.environ.get(name, "")
    if not value or value != value.strip():
        raise ArtifactError(f"{name} is required and must not contain surrounding whitespace")
    return value


def load_context() -> ReleaseContext:
    repository_root = Path(
        run_checked(
            ["git", "rev-parse", "--show-toplevel"],
            cwd=Path.cwd(),
        ).stdout.strip()
    ).resolve()
    version = required_environment("RELEASE_VERSION")
    if VERSION_PATTERN.fullmatch(version) is None:
        raise ArtifactError(f"RELEASE_VERSION is not a canonical release tag: {version}")

    artifact_directory = Path(required_environment("RELEASE_ARTIFACT_DIR"))
    if not artifact_directory.is_absolute():
        raise ArtifactError("RELEASE_ARTIFACT_DIR must be absolute")
    artifact_directory = artifact_directory.resolve()
    staging_path = artifact_directory / "staging.json"
    if not staging_path.is_file() or staging_path.is_symlink():
        raise ArtifactError("release staging area is not initialized")
    try:
        staging = json.loads(staging_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ArtifactError(f"read release staging manifest: {error}") from error

    expected_staging = {
        "schema_version": 1,
        "artifact_kind": "mprlab.release.staging",
        "version": version,
    }
    for key, expected_value in expected_staging.items():
        if staging.get(key) != expected_value:
            raise ArtifactError(
                f"release staging field {key} is {staging.get(key)!r}; "
                f"expected {expected_value!r}"
            )

    source_commit = str(staging.get("source_commit") or "")
    release_timestamp = str(staging.get("release_timestamp") or "")
    if not source_commit or not release_timestamp:
        raise ArtifactError("release staging manifest lacks source identity")
    resolved_source = run_checked(
        ["git", "rev-parse", "--verify", f"{source_commit}^{{commit}}"],
        cwd=repository_root,
    ).stdout.strip()
    if resolved_source != source_commit:
        raise ArtifactError("release staging source commit is not canonical")
    if (
        run_checked(["git", "rev-parse", "HEAD"], cwd=repository_root).stdout.strip()
        != source_commit
    ):
        raise ArtifactError("release artifacts must be built from the staged source commit")
    source_epoch_text = run_checked(
        ["git", "show", "-s", "--format=%ct", source_commit],
        cwd=repository_root,
    ).stdout.strip()
    if not source_epoch_text.isdigit():
        raise ArtifactError("source commit timestamp is invalid")

    return ReleaseContext(
        repository_root=repository_root,
        artifact_directory=artifact_directory,
        version=version,
        source_commit=source_commit,
        release_timestamp=release_timestamp,
        source_epoch=int(source_epoch_text),
    )


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def build_application(context: ReleaseContext, output_path: Path) -> None:
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise ArtifactError(
            "the canonical application artifact must be built and executed on macOS arm64"
        )
    environment = os.environ.copy()
    environment.update(
        {
            "CGO_ENABLED": "1",
            "GOOS": APPLICATION_PLATFORM,
            "GOARCH": APPLICATION_ARCHITECTURE,
        }
    )
    linker_flags = (
        f"-buildid= -s -w -X main.version={context.version}"
    )
    run_checked(
        [
            "go",
            "build",
            "-trimpath",
            "-buildvcs=false",
            f"-ldflags={linker_flags}",
            "-o",
            str(output_path),
            ".",
        ],
        cwd=context.repository_root,
        environment=environment,
    )
    output_path.chmod(0o755)
    file_description = run_checked(
        ["file", str(output_path)],
        cwd=context.repository_root,
    ).stdout
    if "Mach-O 64-bit executable arm64" not in file_description:
        raise ArtifactError(
            f"application artifact has the wrong platform: {file_description.strip()}"
        )
    version_output = run_checked(
        [str(output_path), "version"],
        cwd=context.repository_root,
    ).stdout.strip()
    if version_output != context.version:
        raise ArtifactError(
            f"application artifact version is {version_output!r}; "
            f"expected {context.version!r}"
        )
    help_output = run_checked(
        [str(output_path), "help"],
        cwd=context.repository_root,
    ).stdout
    if "Local workspaces and operator tools for personal data exports." not in help_output:
        raise ArtifactError("application artifact help contract is incomplete")


def add_directory(
    archive: tarfile.TarFile,
    name: str,
    *,
    epoch: int,
) -> None:
    information = tarfile.TarInfo(name=name.rstrip("/") + "/")
    information.type = tarfile.DIRTYPE
    information.mode = 0o755
    information.uid = 0
    information.gid = 0
    information.uname = ""
    information.gname = ""
    information.mtime = epoch
    archive.addfile(information)


def add_bytes(
    archive: tarfile.TarFile,
    name: str,
    contents: bytes,
    *,
    mode: int,
    epoch: int,
) -> None:
    information = tarfile.TarInfo(name=name)
    information.size = len(contents)
    information.mode = mode
    information.uid = 0
    information.gid = 0
    information.uname = ""
    information.gname = ""
    information.mtime = epoch
    archive.addfile(information, io.BytesIO(contents))


def write_archive(
    destination: Path,
    *,
    directories: Sequence[str],
    files: Mapping[str, tuple[bytes, int]],
    epoch: int,
) -> None:
    with destination.open("wb") as raw_handle:
        with gzip.GzipFile(
            filename="",
            mode="wb",
            fileobj=raw_handle,
            mtime=0,
        ) as gzip_handle:
            with tarfile.open(
                mode="w",
                fileobj=gzip_handle,
                format=tarfile.USTAR_FORMAT,
            ) as archive:
                for directory in sorted(directories):
                    add_directory(archive, directory, epoch=epoch)
                for name in sorted(files):
                    contents, mode = files[name]
                    add_bytes(
                        archive,
                        name,
                        contents,
                        mode=mode,
                        epoch=epoch,
                    )


def verify_archive(
    path: Path,
    expected_files: Mapping[str, int],
    expected_directories: Sequence[str],
) -> None:
    with tarfile.open(path, mode="r:gz") as archive:
        members = archive.getmembers()
    actual_files: dict[str, int] = {}
    actual_directories: list[str] = []
    for member in members:
        if member.issym() or member.islnk():
            raise ArtifactError(f"release archive contains a link: {member.name}")
        member_path = Path(member.name)
        if member_path.is_absolute() or ".." in member_path.parts:
            raise ArtifactError(f"release archive contains an unsafe path: {member.name}")
        if member.isdir():
            actual_directories.append(member.name.rstrip("/"))
        elif member.isfile():
            actual_files[member.name] = stat.S_IMODE(member.mode)
        else:
            raise ArtifactError(
                f"release archive contains an unsupported member: {member.name}"
            )
    if actual_files != dict(expected_files):
        raise ArtifactError(
            f"release archive file set is {actual_files}; expected {dict(expected_files)}"
        )
    if sorted(actual_directories) != sorted(expected_directories):
        raise ArtifactError(
            "release archive directory set does not match the canonical package"
        )


def package_application(context: ReleaseContext, temporary_root: Path) -> None:
    first_binary = temporary_root / "application-first"
    second_binary = temporary_root / "application-second"
    build_application(context, first_binary)
    build_application(context, second_binary)
    if sha256(first_binary) != sha256(second_binary):
        raise ArtifactError("two builds from the same source produced different binaries")

    package_root = (
        f"{APPLICATION_NAME}_{context.version}_{APPLICATION_PLATFORM}_"
        f"{APPLICATION_ARCHITECTURE}"
    )
    application_files: dict[str, tuple[bytes, int]] = {
        f"{package_root}/{APPLICATION_NAME}": (first_binary.read_bytes(), 0o755),
    }
    for archive_name, source_name in APPLICATION_DOCUMENTS.items():
        source_path = context.repository_root / source_name
        if not source_path.is_file() or source_path.is_symlink():
            raise ArtifactError(f"application release document is missing: {source_name}")
        application_files[f"{package_root}/{archive_name}"] = (
            source_path.read_bytes(),
            0o644,
        )

    first_archive = temporary_root / "application-first.tar.gz"
    second_archive = temporary_root / "application-second.tar.gz"
    for archive_path in (first_archive, second_archive):
        write_archive(
            archive_path,
            directories=(package_root,),
            files=application_files,
            epoch=context.source_epoch,
        )
    if sha256(first_archive) != sha256(second_archive):
        raise ArtifactError("two packages from the same inputs produced different archives")

    archive_name = f"{package_root}.tar.gz"
    checksum_name = f"{archive_name}.sha256"
    destination = context.asset_directory / archive_name
    checksum_destination = context.asset_directory / checksum_name
    shutil.copyfile(first_archive, destination)
    checksum_destination.write_text(
        f"{sha256(destination)}  {archive_name}\n",
        encoding="utf-8",
    )
    verify_archive(
        destination,
        expected_files={
            f"{package_root}/{APPLICATION_NAME}": 0o755,
            f"{package_root}/README.md": 0o644,
            f"{package_root}/FIRST_RUN.md": 0o644,
            f"{package_root}/LICENSE": 0o644,
        },
        expected_directories=(package_root,),
    )


def package_pages(context: ReleaseContext, temporary_root: Path) -> None:
    pages_root = context.repository_root / ".mprlab" / "deploy" / "pages"
    actual_names = sorted(path.name for path in pages_root.iterdir())
    if actual_names != sorted(PAGES_FILES):
        raise ArtifactError(
            f"Pages source contains {actual_names}; expected {sorted(PAGES_FILES)}"
        )

    marker = {
        "schema_version": 1,
        "release_version": context.version,
        "source_commit": context.source_commit,
        "release_timestamp": context.release_timestamp,
    }
    pages_files: dict[str, tuple[bytes, int]] = {
        ".mprlab-release.json": (
            (json.dumps(marker, indent=2, sort_keys=True) + "\n").encode("utf-8"),
            0o644,
        ),
        ".nojekyll": (b"", 0o644),
    }
    for name in PAGES_FILES:
        source_path = pages_root / name
        if not source_path.is_file() or source_path.is_symlink():
            raise ArtifactError(f"Pages source is missing or unsafe: {name}")
        pages_files[name] = (source_path.read_bytes(), 0o644)

    first_archive = temporary_root / "pages-first.tar.gz"
    second_archive = temporary_root / "pages-second.tar.gz"
    for archive_path in (first_archive, second_archive):
        write_archive(
            archive_path,
            directories=(),
            files=pages_files,
            epoch=context.source_epoch,
        )
    if sha256(first_archive) != sha256(second_archive):
        raise ArtifactError("two Pages packages from the same inputs are different")

    destination = context.asset_directory / "pages.tar.gz"
    shutil.copyfile(first_archive, destination)
    verify_archive(
        destination,
        expected_files={
            ".mprlab-release.json": 0o644,
            ".nojekyll": 0o644,
            "index.html": 0o644,
            "styles.css": 0o644,
        },
        expected_directories=(),
    )


def main() -> int:
    try:
        context = load_context()
        context.asset_directory.mkdir(parents=True, exist_ok=True)
        if any(context.asset_directory.iterdir()):
            raise ArtifactError("release asset directory must be empty")
        with tempfile.TemporaryDirectory(
            prefix="download-your-data-release-"
        ) as temporary_directory:
            temporary_root = Path(temporary_directory)
            package_application(context, temporary_root)
            package_pages(context, temporary_root)
        print(f"Prepared sealed release assets in {context.asset_directory}.")
        return 0
    except (ArtifactError, OSError, ValueError, tarfile.TarError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
