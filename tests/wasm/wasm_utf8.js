module.exports = async ({ exports }) => {
  return `WASM_RESULT=${exports.main(0, 0)}\n`;
};
