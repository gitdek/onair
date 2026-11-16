package live

import "time"

// What the real model's test, in package live_test, needs of the inside:
// the prompts On Air builds, and its checks of what the model writes.

// AnswerFor is what the writer is asked about a question said in a call,
// given a brief, and how what it writes is read: the answer shown, the
// lines it cites, and what came of it.
func AnswerFor(said string, b Brief) (Prompt, func(text string) (string, []Point, string)) {
	q, ok := notice(utterance{text: said})
	if !ok {
		return Prompt{}, nil
	}
	from := passages(q, b, Defaults.AnswerTokens)
	return answerPrompt(q, from), func(text string) (string, []Point, string) {
		shown, cited, o, why := readAnswer(text, len(from))
		switch o {
		case answered:
			var lines []Point
			for _, k := range cited {
				lines = append(lines, from[k-1])
			}
			return shown, lines, "answered"
		case notInArchive:
			return "", nil, "not in the archive"
		}
		return "", nil, "rejected: " + why
	}
}

// CheckFor is what the writer is asked about statements said in a call,
// given a brief fetched just before, and how what it writes is read: the
// panel's line, and what came of it.
func CheckFor(said []string, b Brief) (Prompt, func(text string) (string, string)) {
	f := listening(Defaults)
	f.writes(true)
	f.remember(b, t0)
	for _, s := range said {
		f.state(utterance{text: s, to: t0})
	}
	p, n, _, _, ok := f.checkDue(t0.Add(time.Second))
	if !ok {
		return Prompt{}, nil
	}
	return p, func(text string) (string, string) {
		g := *f
		g.differed = map[string]time.Time{}
		result, _ := g.checkedBy(n, text, nil, t0.Add(2*time.Second))
		return g.panel(t0.Add(2 * time.Second)).Differs.Text, result
	}
}
