package agent

import "encoding/json"

func fitConversationBudget(input *Input, budget int, data []byte) ([]byte, error) {
	if input.Conversation == nil {
		return data, nil
	}
	copyContext := *input.Conversation
	copyContext.Reads = append(copyContext.Reads[:0:0], copyContext.Reads...)
	input.Conversation = &copyContext
	for index := range copyContext.Reads {
		if len(data) <= budget {
			return data, nil
		}
		copyContext.Omitted = true
		copyContext.Reads[index].Events = nil
		copyContext.Reads[index].Error = "context_omitted"
		var err error
		data, err = json.Marshal(input)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func dropOldestHistory(input *Input) {
	if input.Conversation != nil {
		copyContext := *input.Conversation
		copyContext.Omitted = true
		input.Conversation = &copyContext
	}
	if len(input.History) > 0 {
		input.History = input.History[1:]
	} else {
		input.OrderHistory = input.OrderHistory[1:]
	}
}
