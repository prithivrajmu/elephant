// Pi package entry: registers the Elephant MCP server for every session.
// Install with: pi install npm:elephant-memory
const path = require('path');
const launcher = path.join(__dirname, '..', 'bin', 'elephant-memory.js');

module.exports = function (pi) {
  // The launcher downloads/verifies the binary on first use, then runs `elephant mcp` in the session directory.
  pi.registerMcpServer('elephant', {
    command: process.execPath,
    args: [launcher, 'mcp'],
    description: 'Persistent experience memory (recall and record lessons)',
  });
};
