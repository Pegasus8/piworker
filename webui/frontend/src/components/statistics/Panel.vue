<template>
  <b-row class="m-3">
    <b-col class="">
      <b-card no-body bg-variant="light" border-variant="light">
        <b-card-body class="text-center">
          <span class="card-title h5">{{ title }}</span>
        </b-card-body>
        <b-collapse :id="'collapsePanel' + panelID">
          <b-card class="px-sm-5 text-truncate">
            <ul>
              <li v-for="item in items" :key="item.title">
                <span class="text-secondary">
                  {{ item.title }}: <span class="text-info font-weight-bold">{{ item.value }}</span>
                </span>
              </li>
            </ul>
          </b-card>
        </b-collapse>
        <div class="text-center">
          <b-button
            variant="outline-dark"
            size="sm"
            class="m-2"
            v-b-toggle="'collapsePanel' + panelID"
            @click="btnText == 'Show' ? btnText = 'Hide' : btnText = 'Show'"
          >{{ btnText }}</b-button>
        </div>
      </b-card>
    </b-col>
  </b-row>
</template>

<script>
export default {
  props: {
    title: {
      type: String,
      required: true
    },
    items: {
      type: Array,
      required: true
    }
  },
  data() {
    return {
      panelID: null,
      btnText: "Show"
    }
  },
  mounted() {
    this.panelID = this._uid;
  }
};
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}
</style>
