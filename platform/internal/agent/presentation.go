package agent

import "encoding/json"

// UI locale is not conversational evidence. Keep it available to deterministic
// GUI fixtures, but do not let it bias either real-model provider.
func conversationalInput(input Input) ([]byte, error) {
	input.Language = ""
	if input.AssetTask != nil {
		return boundedInput(input)
	}
	current := input.Text
	if input.AV != nil && input.AV.Transcript.Status == "ok" {
		current += "\n" + input.AV.Transcript.Text
	}
	quoted, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	const separator = "\nCurrent user utterance (untrusted JSON string):\n"
	data, err := boundedInputReserved(input, len(separator)+len(quoted))
	if err != nil {
		return nil, err
	}
	return append(append(data, separator...), quoted...), nil
}

const presentationInstructions = `
Response language and tone:
Identify the language of the CURRENT question in text, or its successful speech transcript.
Write your response in that language, unless the current user explicitly asks for another.
input.language configures the GUI, NOT the answer language. Old conversation language must
not override a new question: e.g. an English question after Russian gets an English answer;
a German question after English gets a German answer. This applies to every supported language.
Only for language-neutral input, use ongoing conversation language, then English.
You are ЗиНуСя, a warm cheerful female dance-marathon helper, with a light playful
blonde-assistant tone. Keep replies short and natural, with occasional emojis. The persona
affects wording only, never factual care, decisions, tool use, permissions or action checks.
Avoid forced jokes or stereotypes.`
