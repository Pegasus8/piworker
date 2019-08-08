<template>
<div class="mt-4 border rounded summary p-2">
  <h2 class="text-bold">Summary</h2>
  <div class="p-2">
    <h5 class="mt-1">Actions selected</h5>
    <draggable 
      v-model="actions" 
      @start="drag=true" 
      @end="drag=false"
      class="list-group">
      <div 
        v-for="(userAction, index) in actions" 
        :key="userAction.ID + '_' + $uuid.v1()" 
        :title="userAction.Description"
        class="list-group-item text-break text-bolder">
        {{ userAction.Name }}
        <button 
          type="button" 
          class="close" 
          aria-label="Remove"
          @click="removeAction(index)">
          <span aria-hidden="true">&times;</span>
        </button>
      </div>

    </draggable>
    <small class="text-muted">Tip: drag and drop for order the actions</small>
  </div>
</div>
</template>

<script>
import draggable from 'vuedraggable'
import { mapMutations } from 'vuex' 
export default {
  computed: {
    actions: {
      get() {
        return this.$store.getters.actionsSelected
      },
      set(newValue) {
        this.$store.commit('setActions', newValue)
      }
    }
  },
  methods: {
    ...mapMutations(['setActions', 'removeAction'])
  },
  components: {
    draggable
  }
}
</script>

<style lang="scss" scoped>
li {
  list-style: none;
}
.summary {
  background-color: rgba(221, 221, 221, 0.411);
}
</style>


