package audit

import commonvdoc "vdoc/common/vdoc"

func Build(params BuildParams) *AuditLog {
	actorType := params.ActorType
	if params.Context.ActorType != 0 {
		actorType = params.Context.ActorType
	}
	if actorType == 0 {
		actorType = commonvdoc.AuditActorSystem
	}
	metadata := copyStringMap(params.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if _, ok := metadata["result"]; !ok {
		metadata["result"] = "success"
	}
	return &AuditLog{ID: params.ID, ActorType: actorType, ActorUserID: params.ActorUserID, ActorTokenID: params.Context.ActorTokenID, Action: params.Action, ResourceType: params.ResourceType, ResourceID: params.ResourceID, ProjectID: params.ProjectID, ServiceID: params.ServiceID, Metadata: metadata, IPAddress: params.Context.IPAddress, UserAgent: params.Context.UserAgent, RequestID: params.Context.RequestID, CreatedAt: params.Now, UpdatedAt: params.Now}
}

func Metadata(pairs ...string) map[string]string {
	metadata := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		key := pairs[index]
		if key == "" {
			continue
		}
		metadata[key] = pairs[index+1]
	}
	return metadata
}

func copyStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	copy := make(map[string]string, len(input))
	for key, value := range input {
		copy[key] = value
	}
	return copy
}
