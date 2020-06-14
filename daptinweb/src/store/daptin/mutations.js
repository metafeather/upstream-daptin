import Vue from "vue";

export function setToken(state, token) {
  state.token = token;
}

export function setDefaultCloudStore(state, cloudStore) {
  state.defaultCloudStore = cloudStore;
}

export function setSelectedTable(state, tableName) {
  console.log("set selected table", tableName);
  state.selectedTable = tableName;
}

export function setTables(state, tables) {
  for (var tableName in tables) {
    Vue.set(state.tables, tableName, tables[tableName])
  }
  console.log("Tables set to ", state.tables)
}
