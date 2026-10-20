package curseforge

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/andre-carbajal/packwiz/core"
)

func init() { core.PlatformLinkers["curseforge"] = linkCurseForge }

func linkCurseForge(identity core.FileIdentity, reference string) (map[string]any, error) {
	var projectID, fileID uint32
	if reference != "" {
		u, err := url.Parse(reference)
		if err != nil || u.Scheme != "https" || (u.Hostname() != "www.curseforge.com" && u.Hostname() != "curseforge.com") {
			return nil, fmt.Errorf("expected a CurseForge HTTPS project or file URL")
		}
		game, _, slug, id, err := parseSlugOrUrl(reference)
		if err != nil {
			return nil, err
		}
		if game != "minecraft" || slug == "" {
			return nil, fmt.Errorf("invalid Minecraft CurseForge URL")
		}
		projects, err := cfDefaultClient.getSearch("", slug, 432, 0, 0, "", modloaderTypeAny)
		if err != nil {
			return nil, err
		}
		for _, project := range projects {
			if project.Slug == slug {
				if projectID != 0 {
					return nil, fmt.Errorf("ambiguous CurseForge project URL")
				}
				projectID = project.ID
			}
		}
		if projectID == 0 {
			return nil, fmt.Errorf("CurseForge project not found")
		}
		fileID = id
	}
	res, err := cfDefaultClient.getFingerprintInfo([]uint32{getByteArrayHash(identity.Bytes)})
	if err != nil {
		return nil, err
	}
	var matches []cfUpdateData
	seen := make(map[cfUpdateData]bool)
	for _, candidate := range res.ExactMatches {
		id := candidate.File.ModID
		if id == 0 {
			id = candidate.ID
		}
		if id == 0 || candidate.File.ID == 0 {
			return nil, fmt.Errorf("invalid CurseForge fingerprint response")
		}
		if projectID != 0 && id != projectID {
			continue
		}
		if fileID != 0 && candidate.File.ID != fileID {
			continue
		}
		match, err := identicalCFFile(identity, id, candidate.File)
		if err != nil {
			return nil, err
		}
		metadata := cfUpdateData{ProjectID: id, FileID: candidate.File.ID}
		if match && !seen[metadata] {
			matches = append(matches, metadata)
			seen[metadata] = true
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no CurseForge file with identical contents was found; linking requires the exact same file, so this mod was left unchanged")
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("multiple identical CurseForge files; use packwiz link with --curseforge <file-URL>")
	}
	return matches[0].ToMap()
}

func identicalCFFile(identity core.FileIdentity, projectID uint32, file modFileInfo) (bool, error) {
	for _, hash := range file.Hashes {
		if hash.Algorithm == hashAlgoSHA1 && hash.Value != "" {
			return identity.Hashes["sha1"] != "" && strings.EqualFold(hash.Value, identity.Hashes["sha1"]), nil
		}
	}
	// Fingerprints ignore whitespace: download and compare a cryptographic hash
	// when CurseForge does not provide SHA-1. Never trust a fingerprint alone.
	if file.DownloadURL == "" {
		var err error
		file, err = cfDefaultClient.getFileInfo(projectID, file.ID)
		if err != nil {
			return false, err
		}
	}
	if file.DownloadURL == "" {
		return false, fmt.Errorf("cannot verify CurseForge file %d: no SHA-1 or download URL", file.ID)
	}
	// Do not reuse the fingerprint-keyed cache: whitespace-different files
	// share that key and could otherwise appear to be identical.
	response, err := core.GetWithUA(file.DownloadURL, "application/octet-stream")
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("download returned %s", response.Status)
	}
	hash, err := core.GetHashImpl("sha512")
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(hash, response.Body); err != nil {
		return false, err
	}
	return identity.Hashes["sha512"] != "" && identity.Hashes["sha512"] == hash.HashToString(hash.Sum(nil)), nil
}
