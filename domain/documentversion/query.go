package documentversion

func LatestID(version *ContractVersion) string {
	if version == nil {
		return ""
	}
	return version.ID
}
