module.exports = async ({ exports }) => `WASM_RESULT=${exports.main(0, 0)}\n`;
