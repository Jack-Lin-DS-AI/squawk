Output demo.gif
Set FontSize 13
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Shell zsh

Require squawk
Require curl

Type "squawk setup"
Enter
Sleep 2s

Type "squawk rules list"
Enter
Sleep 3s

Type "# Simulating: Claude Code edits test files 3x without reading source"
Enter
Sleep 1.5s

Type "PORT=3131"
Enter
Sleep 300ms

Type `for i in 1 2 3; do curl -s localhost:$PORT/hooks/post-tool-use -d '{"session_id":"s1","hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{"file_path":"/p/foo_test.go","old_string":"old","new_string":"new"}}' > /dev/null; done; echo "Sent 3 test-file edits (no source reads)"`
Enter
Sleep 2s

Type "# Claude Code now tries to edit another test file..."
Enter
Sleep 1s

Type `curl -s localhost:$PORT/hooks/pre-tool-use -d '{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"/p/bar_test.go"}}' | python3 -m json.tool`
Enter
Sleep 4s

Type "squawk stats"
Enter
Sleep 3s

Type "squawk stop"
Enter
Sleep 1.5s
