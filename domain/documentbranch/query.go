package documentbranch

func InDocument(branch *ContractBranch, documentID string) bool {
	return branch != nil && owningDocumentID(branch) == documentID
}
