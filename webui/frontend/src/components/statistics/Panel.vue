<template>
  <div class="row m-3">
    <div class="col bg-light border border-light card">
      <div class="card-body text-center">
        <span class="card-title h5">{{ title }}</span>
      </div>
      <div class="collapse" :id="'collapsePanel' + panelID">
        <div class="card card-body px-sm-5 text-truncate">
          <ul>
            <li v-for="item in items" :key="item.title">
              <span class="text-secondary">
                {{ item.title }}: <span class="text-info font-weight-bold">{{ item.value }}</span>
              </span>
            </li>
          </ul>
        </div>
      </div>
      <div class="text-center">
        <button
          class="btn btn-outline-dark m-2 btn-sm"
          data-toggle="collapse"
          :data-target="'#collapsePanel' + panelID"
          type="button"
          aria-expanded="true"
          @click="btnText == 'Show' ? btnText = 'Hide' : btnText = 'Show'"
        >{{ btnText }}</button>
      </div>
    </div>
  </div>
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
